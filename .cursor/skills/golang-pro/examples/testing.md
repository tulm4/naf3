# Go Testing Patterns

## Unit Testing with Mocks

```go
// internal/service/auth_test.go
func TestAuthService_CreateContext(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name    string
        req     *CreateAuthReq
        setup   func(*MockAuthRepo, *MockAAAClient)
        check   func(t *testing.T, ctx *AuthContext, err error)
    }{
        {
            name: "success_aka_prime",
            req: &CreateAuthReq{
                Supi: "imsi-460001234567891",
                Snssai: Snssai{SST: 1, SD: "000001"},
                EAPMethod: EAPAKAPrime,
            },
            setup: func(repo *MockAuthRepo, aaa *MockAAAClient) {
                repo.InsertFunc.SetDefaultReturn(nil)
            },
            check: func(t *testing.T, ctx *AuthContext, err error) {
                require.NoError(t, err)
                assert.NotEmpty(t, ctx.ID)
                assert.Equal(t, StateCreated, ctx.State)
            },
        },
        {
            name: "repo_insert_failure",
            req: &CreateAuthReq{Supi: "imsi-001"},
            setup: func(repo *MockAuthRepo, _ *MockAAAClient) {
                repo.InsertFunc.SetDefaultHook(func(_ context.Context, _ *AuthContext) error {
                    return errors.New("connection refused")
                })
            },
            check: func(t *testing.T, _ *AuthContext, err error) {
                require.Error(t, err)
                assert.Contains(t, err.Error(), "insert auth context")
            },
        },
    }

    for _, tt := range tests {
        tt := tt
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            repo := &MockAuthRepo{}
            aaa := &MockAAAClient{}
            tt.setup(repo, aaa)

            svc := NewAuthService(repo, aaa, &MockLogger{})
            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()

            result, err := svc.CreateContext(ctx, tt.req)
            tt.check(t, result, err)
        })
    }
}
```

## Mock Definitions

```go
// internal/service/mock_test.go
type MockAuthRepo struct {
    InsertFunc     func(context.Context, *AuthContext) error
    FindByIDFunc   func(context.Context, AuthCtxID) (*AuthContext, error)
    UpdateStateFunc func(context.Context, AuthCtxID, AuthState) error
}

func (m *MockAuthRepo) Insert(ctx context.Context, a *AuthContext) error {
    if m.InsertFunc != nil {
        return m.InsertFunc(ctx, a)
    }
    return nil
}
func (m *MockAuthRepo) FindByID(ctx context.Context, id AuthCtxID) (*AuthContext, error) {
    if m.FindByIDFunc != nil {
        return m.FindByIDFunc(ctx, id)
    }
    return nil, ErrAuthContextNotFound
}

// Implement the interface
var _ AuthRepository = (*MockAuthRepo)(nil)
```

## HTTP Handler Tests with httptest

```go
func TestCreateAuthContext_Handler(t *testing.T) {
    t.Parallel()

    mockSvc := &MockAuthService{
        CreateContextFunc: func(_ context.Context, req *CreateAuthReq) (*AuthContext, error) {
            return &AuthContext{
                ID:     "test-ctx-id",
                Supi:   req.Supi,
                State:  StatePending,
            }, nil
        },
    }
    handler := NewHandler(mockSvc, &MockValidator{}, NewProblemDetailsEncoder())

    body := `{"supi":"imsi-001","snssai":{"sst":1,"sd":"000001"}}`
    req := httptest.NewRequest(http.MethodPost, "/slice-authentications", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()

    handler.ServeHTTP(rr, req)

    assert.Equal(t, http.StatusCreated, rr.Code)

    var resp CreateAuthContextResponse
    err := json.NewDecoder(rr.Body).Decode(&resp)
    require.NoError(t, err)
    assert.Equal(t, "test-ctx-id", resp.AuthCtxID)
}
```

## Table-Driven Integration Tests

```go
func TestEAPEngine_ProcessRound(t *testing.T) {
    t.Parallel()

    engine := NewEAPEngine(&Config{MaxRounds: 20, SessionTimeout: 120 * time.Second})

    tests := []struct {
        name         string
        initialState EAPState
        inPkt        []byte
        wantState    EAPState
        wantErr      bool
    }{
        {
            name:      "aka_prime_full_auth",
            inPkt:     loadFixture("testdata/eap_aka_identity_resp.raw"),
            wantState: StatePending,
            wantErr:   false,
        },
    }

    for _, tt := range tests {
        tt := tt
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            result, err := engine.Process(context.Background(), tt.inPkt)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
                assert.Equal(t, tt.wantState, result.State)
            }
        })
    }
}
```

## Benchmark Tests

```go
func BenchmarkAuthContext_Insert(b *testing.B) {
    repo := newTestRepo(b)
    ctx := context.Background()
    authCtx := &AuthContext{
        ID:    NewAuthCtxID(),
        Supi:  "imsi-001",
        State: StateCreated,
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        if err := repo.Insert(ctx, authCtx); err != nil {
            b.Fatal(err)
        }
    }
}

func BenchmarkAuthContext_Lookup_Parallel(b *testing.B) {
    repo := newTestRepo(b)
    ctx := context.Background()
    // seed data
    for i := 0; i < 10000; i++ {
        repo.Insert(ctx, &AuthContext{ID: AuthCtxID(fmt.Sprintf("bench-%d", i)), Supi: "imsi-001"})
    }

    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        id := 0
        for pb.Next() {
            repo.FindByID(ctx, AuthCtxID(fmt.Sprintf("bench-%d", id%10000)))
            id++
        }
    })
}
```

## Golden File Tests

```go
func TestProblemDetailsEncoder_Golden(t *testing.T) {
    golden, err := os.ReadFile("testdata/problem_details.golden.json")
    require.NoError(t, err)

    pd := ProblemDetails{
        Type:   "https://example.com/errors/auth-context-not-found",
        Title:  "Authentication Context Not Found",
        Status: 404,
        Detail: "Auth context abc-123 not found",
        Instance: "/nnssaaf-nssaa/v1/slice-authentications/abc-123",
    }

    var buf bytes.Buffer
    err = json.NewEncoder(&buf).Encode(pd)
    require.NoError(t, err)

    assert.JSONEq(t, string(golden), buf.String())
}
```
