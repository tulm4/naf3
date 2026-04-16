// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/operator/nssAAF/internal/types"
)

// ============================================================================
// EAP Packet Parse / Encode
// ============================================================================

func TestParse_validRequest(t *testing.T) {
	// EAP-Request/Identity (Code=1, Id=1, Type=1)
	data := []byte{0x01, 0x01, 0x00, 0x09, 0x01, 0x41, 0x6e, 0x6f, 0x6e}
	pkt, err := Parse(data)

	require.NoError(t, err)
	assert.Equal(t, EapCodeRequest, pkt.Code)
	assert.Equal(t, uint8(1), pkt.Id)
	assert.Equal(t, uint16(9), pkt.Length)
	assert.Equal(t, byte(EapMethodIdentity), pkt.Type)
	assert.Equal(t, []byte("Anon"), pkt.Data)
}

func TestParse_validResponse(t *testing.T) {
	// EAP-Response/Identity (Code=2, Id=1, Type=1)
	data := []byte{0x02, 0x01, 0x00, 0x0a, 0x01, 0x75, 0x73, 0x65, 0x72, 0x40}
	pkt, err := Parse(data)

	require.NoError(t, err)
	assert.Equal(t, EapCodeResponse, pkt.Code)
	assert.Equal(t, uint8(1), pkt.Id)
	assert.Equal(t, uint16(10), pkt.Length)
	assert.Equal(t, byte(EapMethodIdentity), pkt.Type)
	assert.Equal(t, []byte("user@"), pkt.Data)
}

func TestParse_success(t *testing.T) {
	// EAP-Success (Code=3, Id=2, Length=4 — no Type field)
	data := []byte{0x03, 0x02, 0x00, 0x04}
	pkt, err := Parse(data)

	require.NoError(t, err)
	assert.Equal(t, EapCodeSuccess, pkt.Code)
	assert.Equal(t, uint8(2), pkt.Id)
	assert.Equal(t, uint16(4), pkt.Length)
	assert.Equal(t, uint8(0), pkt.Type) // Success has no Type field
	assert.Empty(t, pkt.Data) // codec sets Data from data[4:], which is empty
}

func TestParse_failure(t *testing.T) {
	// EAP-Failure (Code=4, Id=3, Length=4)
	data := []byte{0x04, 0x03, 0x00, 0x04}
	pkt, err := Parse(data)

	require.NoError(t, err)
	assert.Equal(t, EapCodeFailure, pkt.Code)
	assert.Equal(t, uint8(3), pkt.Id)
	assert.Equal(t, uint16(4), pkt.Length)
}

func TestParse_eapTLS(t *testing.T) {
	// EAP-Request/TLS (Code=1, Id=5, Type=13, plus 11 bytes of TLS data)
	// Length field = 16 (4 header + 1 type + 11 data = 16)
	data := []byte{0x01, 0x05, 0x00, 0x10, 0x0d, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a}
	pkt, err := Parse(data)

	require.NoError(t, err)
	assert.Equal(t, EapCodeRequest, pkt.Code)
	assert.Equal(t, uint8(5), pkt.Id)
	assert.Equal(t, byte(EapMethodTLS), pkt.Type)
	assert.Len(t, pkt.Data, 11) // bytes 5-15 = 11 bytes
}

func TestParse_tooShort(t *testing.T) {
	for _, l := range []int{0, 1, 2, 3} {
		_, err := Parse(make([]byte, l))
		assert.ErrorIs(t, err, ErrInvalidPacket, "length=%d", l)
	}
}

func TestParse_lengthMismatch(t *testing.T) {
	data := []byte{0x01, 0x01, 0x00, 0x10, 0x01, 0x41} // header says 16, actual is 6
	_, err := Parse(data)
	assert.ErrorIs(t, err, ErrUnexpectedLength)
}

func TestParse_invalidCode(t *testing.T) {
	data := []byte{0x05, 0x01, 0x00, 0x04} // code 5 is invalid
	_, err := Parse(data)
	assert.Error(t, err)
}

func TestParse_tooLarge(t *testing.T) {
	// Length field says 65535, but data is only 4 bytes.
	data := []byte{0x01, 0x01, 0xff, 0xff}
	_, err := Parse(data)
	assert.ErrorIs(t, err, ErrUnexpectedLength)
}

func TestParse_requestMissingTypeField(t *testing.T) {
	// Request packet with only 4 bytes (missing Type field)
	data := []byte{0x01, 0x01, 0x00, 0x04}
	_, err := Parse(data)
	assert.ErrorIs(t, err, ErrInvalidPacket)
}

func TestParse_emptyData(t *testing.T) {
	_, err := Parse(nil)
	assert.Error(t, err)
}

// ============================================================================
// EAP Packet Encode
// ============================================================================

func TestEncode_successRoundTrip(t *testing.T) {
	pkt := BuildSuccess(42)
	raw := Encode(pkt)

	require.Len(t, raw, 4)
	assert.Equal(t, byte(0x03), raw[0]) // Code=Success
	assert.Equal(t, byte(42), raw[1])   // Id
	assert.Equal(t, []byte{0x00, 0x04}, raw[2:4])

	decoded, err := Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, EapCodeSuccess, decoded.Code)
	assert.Equal(t, uint8(42), decoded.Id)
}

func TestEncode_failureRoundTrip(t *testing.T) {
	pkt := BuildFailure(7)
	raw := Encode(pkt)

	require.Len(t, raw, 4)
	assert.Equal(t, byte(0x04), raw[0]) // Code=Failure

	decoded, err := Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, EapCodeFailure, decoded.Code)
}

func TestEncode_requestIdentity(t *testing.T) {
	pkt := BuildRequest(3, EapMethodIdentity, []byte("user@example.com"))
	raw := Encode(pkt)

	// Length = 4 (header) + 1 (type) + len("user@example.com") = 21
	expectedLen := uint16(4 + 1 + len("user@example.com"))
	assert.Equal(t, expectedLen, pkt.Length)
	assert.Equal(t, expectedLen, binary.BigEndian.Uint16(raw[2:4]))

	decoded, err := Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, EapCodeRequest, decoded.Code)
	assert.Equal(t, uint8(3), decoded.Id)
	assert.Equal(t, byte(EapMethodIdentity), decoded.Type)
	assert.Equal(t, []byte("user@example.com"), decoded.Data)
}

func TestEncode_responseWithData(t *testing.T) {
	data := []byte{0x80, 0x00, 0x00, 0x20} // TLS flags
	pkt := BuildResponse(4, EapMethodTLS, data)
	raw := Encode(pkt)

	decoded, err := Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, EapCodeResponse, decoded.Code)
	assert.Equal(t, uint8(4), decoded.Id)
	assert.Equal(t, byte(EapMethodTLS), decoded.Type)
	assert.Equal(t, data, decoded.Data)
}

func TestEncode_maxSize(t *testing.T) {
	largeData := bytes.Repeat([]byte{0xAB}, MaxEAPSize-5)
	pkt := BuildRequest(1, EapMethodTLS, largeData)
	assert.Equal(t, uint16(MaxEAPSize), pkt.Length)
}

// ============================================================================
// Build helpers
// ============================================================================

func TestBuildRequest(t *testing.T) {
	pkt := BuildRequest(1, EapMethodTLS, []byte{0x01, 0x02})

	assert.Equal(t, EapCodeRequest, pkt.Code)
	assert.Equal(t, uint8(1), pkt.Id)
	assert.Equal(t, byte(EapMethodTLS), pkt.Type)
	assert.Equal(t, uint16(7), pkt.Length)
	assert.NotNil(t, pkt.RawData)
}

func TestBuildResponse(t *testing.T) {
	pkt := BuildResponse(2, EapMethodAKAPrime, []byte("test"))
	assert.Equal(t, EapCodeResponse, pkt.Code)
	assert.Equal(t, uint8(2), pkt.Id)
	assert.Equal(t, byte(EapMethodAKAPrime), pkt.Type)
}

func TestBuildSuccess(t *testing.T) {
	pkt := BuildSuccess(5)
	assert.Equal(t, EapCodeSuccess, pkt.Code)
	assert.Equal(t, uint8(5), pkt.Id)
	assert.Equal(t, uint16(4), pkt.Length)
	assert.Nil(t, pkt.Data)
}

func TestBuildFailure(t *testing.T) {
	pkt := BuildFailure(6)
	assert.Equal(t, EapCodeFailure, pkt.Code)
	assert.Equal(t, uint8(6), pkt.Id)
	assert.Equal(t, uint16(4), pkt.Length)
}

// ============================================================================
// EAP Code / Method / Result
// ============================================================================

func TestEapCodeIsValid(t *testing.T) {
	assert.True(t, EapCodeRequest.IsValid())
	assert.True(t, EapCodeResponse.IsValid())
	assert.True(t, EapCodeSuccess.IsValid())
	assert.True(t, EapCodeFailure.IsValid())
	assert.False(t, EapCode(5).IsValid())
	assert.False(t, EapCode(0).IsValid())
}

func TestEapCodeString(t *testing.T) {
	assert.Equal(t, "REQUEST", EapCodeRequest.String())
	assert.Equal(t, "RESPONSE", EapCodeResponse.String())
	assert.Equal(t, "SUCCESS", EapCodeSuccess.String())
	assert.Equal(t, "FAILURE", EapCodeFailure.String())
	assert.Contains(t, EapCode(99).String(), "UNKNOWN")
}

func TestEapMethodIsValid(t *testing.T) {
	assert.True(t, EapMethodIdentity.IsValid())
	assert.True(t, EapMethodNotification.IsValid())
	assert.True(t, EapMethodNak.IsValid())
	assert.True(t, EapMethodTLS.IsValid())
	assert.True(t, EapMethodTTLS.IsValid())
	assert.True(t, EapMethodPEAP.IsValid())
	assert.True(t, EapMethodAKAPrime.IsValid())
	assert.False(t, EapMethod(99).IsValid())
}

func TestEapMethodString(t *testing.T) {
	assert.Equal(t, "Identity", EapMethodIdentity.String())
	assert.Equal(t, "EAP-TLS", EapMethodTLS.String())
	assert.Equal(t, "EAP-AKA'", EapMethodAKAPrime.String())
	assert.Contains(t, EapMethod(99).String(), "Method")
}

func TestEapMethodIsTunneled(t *testing.T) {
	assert.False(t, EapMethodIdentity.IsTunneled())
	assert.False(t, EapMethodTLS.IsTunneled())
	assert.False(t, EapMethodAKAPrime.IsTunneled())
	assert.True(t, EapMethodTTLS.IsTunneled())
	assert.True(t, EapMethodPEAP.IsTunneled())
}

func TestEapTlsFlags(t *testing.T) {
	var f EapTlsFlags = 0x60 // MoreFragments + Length

	assert.True(t, f.HasMoreFragments())
	assert.True(t, f.HasLength())
	assert.False(t, f.HasMoreFragments() == f.HasLength() && false) // sanity

	f2 := EapTlsFlags(0x20)
	assert.False(t, f2.HasMoreFragments())
	assert.True(t, f2.HasLength())
}

func TestEapResultString(t *testing.T) {
	assert.Equal(t, "CONTINUE", ResultContinue.String())
	assert.Equal(t, "SUCCESS", ResultSuccess.String())
	assert.Equal(t, "FAILURE", ResultFailure.String())
	assert.Equal(t, "IGNORED", ResultIgnored.String())
	assert.Equal(t, "TIMEOUT", ResultTimeout.String())
}

// ============================================================================
// Session State
// ============================================================================

func TestSessionStateIsTerminal(t *testing.T) {
	assert.False(t, SessionStateIdle.IsTerminal())
	assert.False(t, SessionStateInit.IsTerminal())
	assert.False(t, SessionStateEapExchange.IsTerminal())
	assert.False(t, SessionStateCompleting.IsTerminal())
	assert.True(t, SessionStateDone.IsTerminal())
	assert.True(t, SessionStateFailed.IsTerminal())
	assert.True(t, SessionStateTimeout.IsTerminal())
}

func TestSessionStateString(t *testing.T) {
	assert.Equal(t, "IDLE", SessionStateIdle.String())
	assert.Equal(t, "INIT", SessionStateInit.String())
	assert.Equal(t, "EAP_EXCHANGE", SessionStateEapExchange.String())
	assert.Equal(t, "COMPLETING", SessionStateCompleting.String())
	assert.Equal(t, "DONE", SessionStateDone.String())
	assert.Equal(t, "FAILED", SessionStateFailed.String())
	assert.Equal(t, "TIMEOUT", SessionStateTimeout.String())
}

// ============================================================================
// Session Lifecycle
// ============================================================================

func TestNewSession(t *testing.T) {
	session := NewSession("auth-123", "user@example.com")

	assert.Equal(t, "auth-123", session.AuthCtxId)
	assert.Equal(t, "user@example.com", session.Gpsi)
	assert.Equal(t, SessionStateInit, session.State)
	assert.Equal(t, DefaultMaxRounds, session.MaxRounds)
	assert.Equal(t, DefaultRoundTimeout, session.Timeout)
	assert.Nil(t, session.CachedResponse)
	assert.Nil(t, session.LastNonce)
	assert.Nil(t, session.TLSState)
}

func TestSessionAdvanceToExchange(t *testing.T) {
	session := NewSession("auth-1", "gpsi@test")

	err := session.AdvanceToExchange()
	require.NoError(t, err)
	assert.Equal(t, SessionStateEapExchange, session.State)
	assert.Equal(t, 1, session.Rounds)
	assert.NotZero(t, session.LastActivity)

	// Cannot advance from DONE.
	session.MarkDone()
	err = session.AdvanceToExchange()
	assert.Error(t, err)
}

func TestSessionAdvanceFromIdle(t *testing.T) {
	session := NewSession("auth-2", "gpsi@test")
	session.State = SessionStateIdle

	err := session.AdvanceToExchange()
	require.NoError(t, err)
	assert.Equal(t, SessionStateEapExchange, session.State)
}

func TestSessionRecordResponse(t *testing.T) {
	session := NewSession("auth-1", "gpsi@test")
	session.AdvanceToExchange()

	payload := []byte("test-response")
	session.RecordResponse(5, payload)

	assert.Equal(t, uint8(6), session.ExpectedId) // Id+1
	assert.Equal(t, 2, session.Rounds)
	assert.NotNil(t, session.LastNonce)
	assert.Len(t, session.LastNonce, 32) // SHA-256
}

func TestSessionCacheResponse(t *testing.T) {
	session := NewSession("auth-1", "gpsi@test")

	response := []byte{0x03, 0x05, 0x00, 0x04} // EAP-Success
	session.CacheResponse(response)

	assert.Equal(t, response, session.CachedResponse)
}

func TestSessionMarkDone(t *testing.T) {
	session := NewSession("auth-1", "gpsi@test")
	session.MarkDone()

	assert.Equal(t, SessionStateDone, session.State)
	assert.NotZero(t, session.LastActivity)
}

func TestSessionMarkFailed(t *testing.T) {
	session := NewSession("auth-2", "gpsi@test")
	session.MarkFailed()

	assert.Equal(t, SessionStateFailed, session.State)
}

func TestSessionMarkTimeout(t *testing.T) {
	session := NewSession("auth-3", "gpsi@test")
	session.MarkTimeout()

	assert.Equal(t, SessionStateTimeout, session.State)
}

func TestSessionIsExpired(t *testing.T) {
	session := NewSession("auth-1", "gpsi@test")

	// Not expired yet.
	assert.False(t, session.IsExpired(time.Hour))

	// Expired (CreatedAt is far in the past).
	session.CreatedAt = time.Now().Add(-2 * time.Hour)
	assert.True(t, session.IsExpired(time.Hour))
}

func TestSessionIsTimedOut(t *testing.T) {
	session := NewSession("auth-1", "gpsi@test")
	session.Timeout = 100 * time.Millisecond

	assert.False(t, session.IsTimedOut())

	time.Sleep(150 * time.Millisecond)
	assert.True(t, session.IsTimedOut())
}

func TestSessionWithOptions(t *testing.T) {
	session := NewSession("auth-123", "gpsi@test").
		WithSnssai("1:ABCDEF").
		WithMaxRounds(10).
		WithTimeout(60 * time.Second)

	assert.Equal(t, "1:ABCDEF", session.SnssaiKey)
	assert.Equal(t, 10, session.MaxRounds)
	assert.Equal(t, 60*time.Second, session.Timeout)
}

func TestSessionString(t *testing.T) {
	session := NewSession("auth-1", "gpsi@test")
	session.Rounds = 3

	s := session.String()
	assert.Contains(t, s, "auth-1")
	assert.Contains(t, s, "INIT")
	assert.Contains(t, s, "3/")
}

// ============================================================================
// Session Manager
// ============================================================================

func TestSessionManagerGetNotFound(t *testing.T) {
	mgr := NewTestSessionManager(10 * time.Minute)
	_, err := mgr.TestGet("nonexistent")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManagerPutGet(t *testing.T) {
	mgr := NewTestSessionManager(10 * time.Minute)
	session := NewTestSession("auth-1", "user@example.com")

	mgr.TestPut(session)
	got, err := mgr.TestGet("auth-1")

	require.NoError(t, err)
	assert.Equal(t, session, got)
	assert.Equal(t, 1, mgr.TestSize())
}

func TestSessionManagerUpdate(t *testing.T) {
	mgr := NewTestSessionManager(10 * time.Minute)
	session1 := NewTestSession("auth-1", "user1@test")
	session2 := NewTestSession("auth-1", "user2@test")

	mgr.TestPut(session1)
	mgr.TestPut(session2) // update

	got, err := mgr.TestGet("auth-1")
	require.NoError(t, err)
	assert.Equal(t, "user2@test", got.Gpsi)
	assert.Equal(t, 1, mgr.TestSize()) // still 1, not 2
}

func TestSessionManagerDelete(t *testing.T) {
	mgr := NewTestSessionManager(10 * time.Minute)
	session := NewTestSession("auth-del", "user@test")
	mgr.TestPut(session)

	mgr.delete("auth-del")
	_, err := mgr.TestGet("auth-del")
	assert.ErrorIs(t, err, ErrSessionNotFound)
	assert.Equal(t, 0, mgr.TestSize())
}

func TestSessionManagerExpired(t *testing.T) {
	mgr := NewTestSessionManager(100 * time.Millisecond) // 100ms TTL

	session := NewTestSession("auth-1", "user@example.com")
	mgr.TestPut(session)

	// Should exist initially.
	_, err := mgr.TestGet("auth-1")
	assert.NoError(t, err)

	// Wait for expiry (200ms > 100ms TTL).
	time.Sleep(200 * time.Millisecond)
	_, err = mgr.TestGet("auth-1")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManagerCleanup(t *testing.T) {
	mgr := NewTestSessionManager(100 * time.Millisecond) // 100ms TTL

	// Each session uses a different authCtxId so they don't overwrite each other.
	for i := 0; i < 5; i++ {
		session := NewTestSession(fmt.Sprintf("auth-expire-%d", i), "user@test")
		mgr.TestPut(session)
		time.Sleep(200 * time.Millisecond) // each is older than previous
	}

	// Put a fresh one.
	fresh := NewTestSession("auth-fresh", "user@test")
	mgr.TestPut(fresh)

	// All 5 expired sessions should be cleaned; fresh one remains.
	count := mgr.cleanup()
	assert.Equal(t, 5, count) // all 5 expired sessions cleaned
	assert.Equal(t, 1, mgr.TestSize()) // only fresh session remains
}

// ============================================================================
// Fragment Buffer
// ============================================================================

func TestFragmentBufferAddFragment(t *testing.T) {
	buf := NewFragmentBuffer("auth-123", 1)

	// Total: "helloworld!" = 11 bytes
	err := buf.SetTotalLength(11)
	require.NoError(t, err)

	// First fragment: 5 bytes.
	_, err = buf.AddFragment(0, []byte("hello"), true)
	require.NoError(t, err)
	assert.False(t, buf.Complete)
	assert.Equal(t, uint32(5), buf.Received)

	// Second fragment: 5 more = 10 total.
	_, err = buf.AddFragment(1, []byte("world"), true)
	require.NoError(t, err)
	assert.False(t, buf.Complete)
	assert.Equal(t, uint32(10), buf.Received)

	// Last fragment: 1 more = 11 total (exact match).
	data, err := buf.AddFragment(2, []byte("!"), false)
	require.NoError(t, err)
	assert.True(t, buf.Complete)
	assert.Equal(t, "helloworld!", string(data))
}

func TestFragmentBufferReassembleWithoutLength(t *testing.T) {
	buf := NewFragmentBuffer("auth-123", 2)

	_, err := buf.AddFragment(0, []byte("test"), true)
	require.NoError(t, err)

	data, err := buf.AddFragment(1, []byte("data"), false)
	require.NoError(t, err)
	assert.True(t, buf.Complete)
	assert.Equal(t, "testdata", string(data))
}

func TestFragmentBufferMissingFragment(t *testing.T) {
	buf := NewFragmentBuffer("auth-123", 3)
	err := buf.SetTotalLength(50)
	require.NoError(t, err)

	_, err = buf.AddFragment(0, []byte("part1"), true)
	require.NoError(t, err)

	_, err = buf.Reassemble()
	assert.Error(t, err)
}

func TestFragmentBufferOverflow(t *testing.T) {
	buf := NewFragmentBuffer("auth-123", 1)
	err := buf.SetTotalLength(5)
	require.NoError(t, err)

	_, err = buf.AddFragment(0, []byte("hello"), true)
	require.NoError(t, err)

	// Send 5 bytes when total is 5 — but seq=1 means another 5 bytes = overflow.
	_, err = buf.AddFragment(1, []byte("world"), false)
	assert.Error(t, err) // received > total
}

func TestFragmentBufferIncomplete(t *testing.T) {
	buf := NewFragmentBuffer("auth-123", 1)
	err := buf.SetTotalLength(20)
	require.NoError(t, err)

	_, err = buf.AddFragment(0, []byte("short"), true)
	require.NoError(t, err)

	// Last fragment but total length not met.
	_, err = buf.AddFragment(1, []byte("!"), false)
	assert.Error(t, err)
}

func TestFragmentBufferDuplicate(t *testing.T) {
	buf := NewFragmentBuffer("auth-123", 1)
	_, err := buf.AddFragment(0, []byte("data"), false)
	require.NoError(t, err)
	assert.True(t, buf.Complete)

	// Try to add more.
	_, err = buf.AddFragment(1, []byte("more"), false)
	assert.Error(t, err)
}

func TestFragmentBufferSetTotalLengthAfterData(t *testing.T) {
	buf := NewFragmentBuffer("auth-123", 1)
	_, err := buf.AddFragment(0, []byte("data"), true)
	require.NoError(t, err)

	err = buf.SetTotalLength(100)
	assert.Error(t, err)
}

func TestFragmentBufferReset(t *testing.T) {
	buf := NewFragmentBuffer("auth-123", 1)
	buf.SetTotalLength(20)
	buf.AddFragment(0, []byte("hello"), false)

	buf.Reset()
	assert.Equal(t, uint16(0), buf.FragmentSeq)
	assert.Equal(t, uint32(0), buf.Received)
	assert.Equal(t, uint32(0), buf.TotalLength)
	assert.False(t, buf.Complete)
	assert.Equal(t, 0, buf.Size())
}

func TestFragmentBufferSize(t *testing.T) {
	buf := NewFragmentBuffer("auth-123", 1)
	assert.Equal(t, 0, buf.Size())

	buf.AddFragment(0, []byte("hello"), true)
	assert.Equal(t, 1, buf.Size())

	buf.AddFragment(1, []byte("world"), false)
	assert.Equal(t, 2, buf.Size())
}

// ============================================================================
// Fragment Manager
// ============================================================================

func TestFragmentManagerGetOrCreate(t *testing.T) {
	mgr := NewFragmentManager(60)

	// First call creates.
	buf1, created := mgr.GetOrCreate("auth-1", 1)
	assert.True(t, created)
	assert.Equal(t, 1, mgr.Size())

	// Second call returns existing.
	buf2, created := mgr.GetOrCreate("auth-1", 1)
	assert.False(t, created)
	assert.Equal(t, buf1, buf2)
}

func TestFragmentManagerGetOrCreateDifferentId(t *testing.T) {
	mgr := NewFragmentManager(60)

	buf1, created := mgr.GetOrCreate("auth-1", 1)
	assert.True(t, created)

	buf2, created := mgr.GetOrCreate("auth-1", 2)
	assert.True(t, created)
	assert.NotEqual(t, buf1, buf2)
	assert.Equal(t, 2, mgr.Size())
}

func TestFragmentManagerGet(t *testing.T) {
	mgr := NewFragmentManager(60)

	buf, created := mgr.GetOrCreate("auth-1", 1)
	require.True(t, created)

	got := mgr.Get("auth-1", 1)
	assert.Equal(t, buf, got)

	// Non-existent.
	assert.Nil(t, mgr.Get("auth-none", 1))
}

func TestFragmentManagerDelete(t *testing.T) {
	mgr := NewFragmentManager(60)

	mgr.GetOrCreate("auth-1", 1)
	mgr.GetOrCreate("auth-1", 2)
	assert.Equal(t, 2, mgr.Size())

	mgr.Delete("auth-1", 1)
	assert.Equal(t, 1, mgr.Size())

	mgr.Delete("auth-1", 2)
	assert.Equal(t, 0, mgr.Size())
}

func TestFragmentManagerCleanup(t *testing.T) {
	// TTL of 1 second.
	mgr := NewFragmentManager(1)

	// Create buffer.
	buf, created := mgr.GetOrCreate("auth-1", 1)
	require.True(t, created)
	require.NotNil(t, buf)
	assert.Equal(t, 1, mgr.Size())

	// Verify CreatedAt was set.
	assert.Greater(t, buf.CreatedAt, int64(0))

	// Immediately clean — nothing should be removed (not expired yet).
	count := mgr.Cleanup()
	assert.Equal(t, 0, count)
	assert.Equal(t, 1, mgr.Size())

	// Force-expire: manipulate CreatedAt to be in the past.
	buf.CreatedAt = nowUnixImpl() - 10 // 10 seconds ago
	count = mgr.Cleanup()
	assert.Equal(t, 1, count)
	assert.Equal(t, 0, mgr.Size())
}

func TestFragmentManagerSize(t *testing.T) {
	mgr := NewFragmentManager(60)
	assert.Equal(t, 0, mgr.Size())

	for i := uint8(1); i <= 10; i++ {
		mgr.GetOrCreate("auth-1", i)
	}
	assert.Equal(t, 10, mgr.Size())
}

// ============================================================================
// Packet Fragmentation
// ============================================================================

func TestSplitPacketNoFragment(t *testing.T) {
	data := []byte("short")
	frags := SplitPacket(data, 4096)

	assert.Len(t, frags, 1)
	assert.Equal(t, uint16(0), frags[0].Seq)
	assert.Equal(t, data, frags[0].Data)
	assert.False(t, frags[0].MoreComing)
}

func TestSplitPacketExactSize(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 253)
	frags := SplitPacket(data, 253)

	assert.Len(t, frags, 1)
	assert.Equal(t, data, frags[0].Data)
	assert.False(t, frags[0].MoreComing)
}

func TestSplitPacketMultipleFragments(t *testing.T) {
	data := bytes.Repeat([]byte("A"), 1000)
	frags := SplitPacket(data, 253)

	assert.Len(t, frags, 4)
	for i := 0; i < len(frags)-1; i++ {
		assert.Equal(t, uint16(i), frags[i].Seq)
		assert.Len(t, frags[i].Data, 253)
		assert.True(t, frags[i].MoreComing)
	}
	last := frags[len(frags)-1]
	assert.Equal(t, uint16(3), last.Seq)
	assert.Len(t, last.Data, 241) // 1000 - 3*253 = 241
	assert.False(t, last.MoreComing)

	// Reassemble.
	result := bytes.Join([][]byte{}, []byte{})
	for _, f := range frags {
		result = append(result, f.Data...)
	}
	assert.Equal(t, data, result)
}

func TestSplitPacketOddSize(t *testing.T) {
	data := bytes.Repeat([]byte("B"), 500)
	frags := SplitPacket(data, 253)

	assert.Len(t, frags, 2)
	assert.Len(t, frags[0].Data, 253)
	assert.Len(t, frags[1].Data, 247)
	assert.False(t, frags[1].MoreComing)
}

// ============================================================================
// Fragment helper
// ============================================================================

func TestFragmentIsLast(t *testing.T) {
	f1 := &Fragment{Seq: 0, Data: []byte("test"), MoreComing: true}
	f2 := &Fragment{Seq: 1, Data: []byte("!"), MoreComing: false}

	assert.False(t, f1.IsLast())
	assert.True(t, f2.IsLast())
}

// ============================================================================
// TLS Session State
// ============================================================================

func TestNewTLSSessionState(t *testing.T) {
	state := NewTLSSessionState()
	assert.False(t, state.HandshakeComplete)
	assert.Nil(t, state.MSK)
	assert.Nil(t, state.TLSData)
}

func TestTLSSessionStateAppendTLSData(t *testing.T) {
	state := NewTLSSessionState()

	state.AppendTLSData([]byte("hello"))
	state.AppendTLSData([]byte("world"))
	assert.Equal(t, []byte("helloworld"), state.TLSData)
}

func TestTLSSessionStateResetTLSData(t *testing.T) {
	state := NewTLSSessionState()
	state.AppendTLSData([]byte("data"))
	assert.NotEmpty(t, state.TLSData)

	state.ResetTLSData()
	assert.Empty(t, state.TLSData)
}

// ============================================================================
// TLS Derivation (negative / error cases)
// ============================================================================

func TestDeriveMSKNilConnectionState(t *testing.T) {
	_, err := DeriveMSK(nil)
	assert.Error(t, err)
}

func TestDeriveMSKUnsupportedVersion(t *testing.T) {
	// TLS 1.0 is not supported.
	connState := &tls.ConnectionState{Version: tls.VersionTLS10}
	_, err := DeriveMSK(connState)
	assert.Error(t, err)
}

// ============================================================================
// Utility Functions
// ============================================================================

func TestBytesEqual(t *testing.T) {
	assert.True(t, bytesEqual([]byte("test"), []byte("test")))
	assert.True(t, bytesEqual([]byte(""), []byte("")))
	assert.False(t, bytesEqual([]byte("test"), []byte("Test")))
	assert.False(t, bytesEqual([]byte("test"), []byte("test1")))
	assert.False(t, bytesEqual([]byte(""), []byte("test")))
	assert.False(t, bytesEqual([]byte("test"), []byte("")))
}

func TestSha256Hash(t *testing.T) {
	h := sha256Hash([]byte("hello"))
	assert.Len(t, h, 32)
	assert.NotNil(t, h)

	h2 := sha256Hash([]byte("hello"))
	assert.Equal(t, h, h2)

	h3 := sha256Hash([]byte("world"))
	assert.NotEqual(t, h, h3)

	h4 := sha256Hash(nil)
	assert.Len(t, h4, 32)
}

func TestAuthResultFromEapResult(t *testing.T) {
	assert.Equal(t, types.AuthResultSuccess, authResultFromEapResult(ResultSuccess))
	assert.Equal(t, types.AuthResultFailure, authResultFromEapResult(ResultFailure))
	assert.Equal(t, types.AuthResultPending, authResultFromEapResult(ResultContinue))
	assert.Equal(t, types.AuthResultPending, authResultFromEapResult(ResultIgnored))
	assert.Equal(t, types.AuthResultFailure, authResultFromEapResult(ResultTimeout))
}

// ============================================================================
// Constants
// ============================================================================

func TestConstants(t *testing.T) {
	assert.Equal(t, 20, DefaultMaxRounds)
	assert.Equal(t, 30*time.Second, DefaultRoundTimeout)
	assert.Equal(t, 10*time.Minute, DefaultSessionTTL)
	assert.Equal(t, 64, MSKLength)
	assert.Equal(t, "EAP-TLS MSK", MSKLabel)
}

// ============================================================================
// Errors
// ============================================================================

func TestErrors(t *testing.T) {
	assert.Error(t, ErrSessionNotFound)
	assert.Error(t, ErrSessionAlreadyDone)
	assert.Error(t, ErrEapIdMismatch)
	assert.Error(t, ErrMaxRoundsExceeded)
	assert.Error(t, ErrSessionTimeout)
	assert.Error(t, ErrInvalidStateTransition)
	assert.Error(t, ErrMissingAuthCtxId)
	assert.Error(t, ErrInvalidPacket)
	assert.Error(t, ErrUnexpectedLength)
	assert.Error(t, ErrPayloadTooLarge)
	assert.Error(t, ErrFragmentIncomplete)
	assert.Error(t, ErrFragmentOverflow)
	assert.Error(t, ErrFragmentNotFound)
	assert.Error(t, ErrFragmentExpire)
	assert.Error(t, ErrMSKDerivationFailed)
	assert.Error(t, ErrTLSVersionNotSupported)
	assert.Error(t, ErrNoAAAClient)
}

// ============================================================================
// Packet Clone
// ============================================================================

func TestPacketClone(t *testing.T) {
	pkt := BuildRequest(1, EapMethodTLS, []byte("test"))
	clone := pkt.Clone()

	assert.Equal(t, pkt.Code, clone.Code)
	assert.Equal(t, pkt.Id, clone.Id)
	assert.Equal(t, pkt.Type, clone.Type)
	assert.Equal(t, pkt.Data, clone.Data)

	// Ensure independent slices.
	clone.Data[0] = 0
	assert.NotEqual(t, pkt.Data[0], clone.Data[0])
}
