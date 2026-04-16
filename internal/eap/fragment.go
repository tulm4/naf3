// Package eap provides EAP (Extensible Authentication Protocol) engine implementation.
// Spec: TS 33.501 §5.13, RFC 3748
package eap

import (
	"errors"
	"fmt"
	"sync"
)

// ErrFragmentIncomplete is returned when trying to reassemble an incomplete fragment buffer.
var ErrFragmentIncomplete = errors.New("eap: fragment buffer incomplete")

// ErrFragmentOverflow is returned when fragments exceed the declared total length.
var ErrFragmentOverflow = errors.New("eap: fragment overflow")

// ErrFragmentNotFound is returned when a fragment number is missing.
var ErrFragmentNotFound = errors.New("eap: fragment not found")

// ErrFragmentExpire is returned when a fragment buffer has expired.
var ErrFragmentExpire = errors.New("eap: fragment buffer expired")

// MaxFragmentSize is the maximum size of a single EAP fragment (4KB).
// This is the default recommended for EAP fragmentation.
// Spec: RFC 3748 §3.5
const MaxFragmentSize = 4096

// MaxFragmentsPerSession is the maximum number of fragments per session.
const MaxFragmentsPerSession = 16

// FragmentBuffer holds fragments for a single EAP message reassembly.
// Spec: RFC 3748 §3.5
type FragmentBuffer struct {
	AuthCtxId    string
	ExpectedId   uint8
	FragmentSeq  uint16 // current fragment sequence number
	TotalLength  uint32 // declared total length (from L flag), 0 if unknown
	Received     uint32 // total bytes received so far
	Fragments    map[uint16][]byte
	Complete     bool
	CreatedAt    int64 // Unix timestamp for TTL
}

// NewFragmentBuffer creates a new fragment buffer for a session.
func NewFragmentBuffer(authCtxId string, id uint8) *FragmentBuffer {
	return &FragmentBuffer{
		AuthCtxId:   authCtxId,
		ExpectedId:  id,
		FragmentSeq: 0,
		Fragments:   make(map[uint16][]byte),
		Complete:    false,
		CreatedAt:  nowUnix(),
	}
}

// AddFragment adds a fragment to the buffer.
// Returns nil if more fragments are expected; returns the complete payload
// if this was the last fragment and reassembly is complete.
func (fb *FragmentBuffer) AddFragment(seq uint16, data []byte, moreFragments bool) ([]byte, error) {
	if fb.Complete {
		return nil, fmt.Errorf("fragment buffer already complete")
	}

	if seq != fb.FragmentSeq {
		return nil, fmt.Errorf("%w: expected seq=%d, got seq=%d",
			ErrFragmentNotFound, fb.FragmentSeq, seq)
	}

	fb.Fragments[fb.FragmentSeq] = data
	fb.Received += uint32(len(data))
	fb.FragmentSeq++

	if !moreFragments {
		// No more fragments coming: complete if total length matches or was unknown.
		if fb.TotalLength > 0 {
			if fb.Received > fb.TotalLength {
				return nil, fmt.Errorf("%w: received %d bytes, expected %d",
					ErrFragmentOverflow, fb.Received, fb.TotalLength)
			}
			if fb.Received < fb.TotalLength {
				return nil, fmt.Errorf("%w: received %d bytes, expected %d",
					ErrFragmentIncomplete, fb.Received, fb.TotalLength)
			}
		}
		fb.Complete = true
		return fb.Reassemble()
	}

	// More fragments coming. If we know the total length and have enough, mark complete.
	if fb.TotalLength > 0 && fb.Received >= fb.TotalLength {
		fb.Complete = true
		return fb.Reassemble()
	}

	// Still waiting for more fragments.
	return nil, nil
}

// SetTotalLength sets the declared total length from the L flag in the first fragment.
// Can only be called before any data is added.
func (fb *FragmentBuffer) SetTotalLength(length uint32) error {
	if fb.Received > 0 {
		return errors.New("cannot set total length after receiving data")
	}
	fb.TotalLength = length
	return nil
}

// Reassemble concatenates all fragments in order into a single payload.
// Returns ErrFragmentIncomplete if not all fragments have been received.
func (fb *FragmentBuffer) Reassemble() ([]byte, error) {
	if !fb.Complete {
		return nil, fmt.Errorf("%w: received %d/%d bytes",
			ErrFragmentIncomplete, fb.Received, fb.TotalLength)
	}

	// TotalLength validation already happened in AddFragment.
	// FragmentSeq tracks how many fragments we've actually received.
	expectedCount := fb.FragmentSeq

	// Validate all expected fragments are present.
	for seq := uint16(0); seq < expectedCount; seq++ {
		if _, ok := fb.Fragments[seq]; !ok {
			return nil, fmt.Errorf("%w: missing seq=%d", ErrFragmentNotFound, seq)
		}
	}

	// Concatenate fragments.
	result := make([]byte, 0, fb.Received)
	for seq := uint16(0); seq < expectedCount; seq++ {
		result = append(result, fb.Fragments[seq]...)
	}
	return result, nil
}

// Reset clears all fragments and resets the buffer.
func (fb *FragmentBuffer) Reset() {
	fb.FragmentSeq = 0
	fb.Received = 0
	fb.TotalLength = 0
	fb.Complete = false
	fb.Fragments = make(map[uint16][]byte)
}

// Size returns the number of fragments currently stored.
func (fb *FragmentBuffer) Size() int {
	return len(fb.Fragments)
}

// Manager maintains fragment buffers for multiple concurrent sessions.
// Thread-safe.
type FragmentManager struct {
	mu       sync.RWMutex
	buffers  map[string]*FragmentBuffer
	ttl      int64 // TTL in seconds
	maxAge   int64 // maximum age before expiry
}

// NewFragmentManager creates a new fragment manager with the given TTL in seconds.
func NewFragmentManager(ttlSeconds int64) *FragmentManager {
	return &FragmentManager{
		buffers: make(map[string]*FragmentBuffer),
		ttl:     ttlSeconds,
		maxAge:  ttlSeconds,
	}
}

// fragmentKey creates a unique key for a fragment buffer.
func fragmentKey(authCtxId string, id uint8) string {
	return fmt.Sprintf("%s:%d", authCtxId, id)
}

// GetOrCreate returns an existing buffer or creates a new one.
func (m *FragmentManager) GetOrCreate(authCtxId string, id uint8) (*FragmentBuffer, bool) {
	key := fragmentKey(authCtxId, id)
	m.mu.Lock()
	defer m.mu.Unlock()

	if buf, ok := m.buffers[key]; ok {
		return buf, false
	}
	buf := NewFragmentBuffer(authCtxId, id)
	buf.CreatedAt = nowUnix()
	m.buffers[key] = buf
	return buf, true
}

// Get returns an existing buffer.
func (m *FragmentManager) Get(authCtxId string, id uint8) *FragmentBuffer {
	key := fragmentKey(authCtxId, id)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.buffers[key]
}

// Delete removes a buffer.
func (m *FragmentManager) Delete(authCtxId string, id uint8) {
	key := fragmentKey(authCtxId, id)
	m.mu.Lock()
	delete(m.buffers, key)
	m.mu.Unlock()
}

// Cleanup removes expired buffers.
// Should be called periodically by a background goroutine.
func (m *FragmentManager) Cleanup() int {
	now := nowUnix()
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for key, buf := range m.buffers {
		if now-buf.CreatedAt > m.maxAge {
			delete(m.buffers, key)
			count++
		}
	}
	return count
}

// Size returns the number of active fragment buffers.
func (m *FragmentManager) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.buffers)
}

// Fragment represents a parsed EAP-TLS fragment from a fragment buffer.
type Fragment struct {
	Seq        uint16
	Data       []byte
	MoreComing bool
}

// IsLast returns true if this is the last fragment.
func (f *Fragment) IsLast() bool {
	return !f.MoreComing
}

// SplitPacket splits a large EAP payload into fragments.
// The first fragment has the L flag set if total length is known.
// Spec: RFC 3748 §3.5
func SplitPacket(packet []byte, maxSize int) []Fragment {
	if len(packet) <= maxSize {
		return []Fragment{{Seq: 0, Data: packet, MoreComing: false}}
	}

	frags := make([]Fragment, 0, (len(packet)+maxSize-1)/maxSize)
	remaining := len(packet)
	offset := 0
	seq := uint16(0)

	for remaining > 0 {
		size := maxSize
		if size > remaining {
			size = remaining
		}

		moreComing := remaining-size > 0
		frags = append(frags, Fragment{
			Seq:        seq,
			Data:       packet[offset : offset+size],
			MoreComing: moreComing,
		})

		offset += size
		remaining -= size
		seq++
	}

	return frags
}

// nowUnix returns the current Unix timestamp in seconds.
func nowUnix() int64 {
	return nowUnixImpl()
}
