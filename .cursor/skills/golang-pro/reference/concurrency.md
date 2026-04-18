# Go Concurrency Patterns

## Context Cancellation

```go
// ✅ GOOD — propagate cancellation
func DoWork(ctx context.Context) error {
    ctx, cancel := context.WithCancel(ctx)
    defer cancel()

    resultCh := make(chan Result, 1)
    go func() {
        resultCh <- expensiveOperation(ctx)
    }()

    select {
    case res := <-resultCh:
        return res.Err
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

## Worker Pool

```go
func WorkerPool(ctx context.Context, jobs <-chan Job, workers int) <-chan Result {
    results := make(chan Result, workers)
    var wg sync.WaitGroup

    for range workers {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for job := range jobs {
                select {
                case results <- processJob(ctx, job):
                case <-ctx.Done():
                    return
                }
            }
        }()
    }

    go func() {
        wg.Wait()
        close(results)
    }()
    return results
}
```

## errgroup for Fan-Out

```go
func FanOut(ctx context.Context, items []Item) error {
    g, ctx := errgroup.WithContext(ctx)
    for _, item := range items {
        item := item
        g.Go(func() error {
            return processOne(ctx, item)
        })
    }
    return g.Wait()
}
```

## Once for Singleton

```go
var (
    instance     *Singleton
    initOnce    sync.Once
    initErr     error
)

func GetInstance() (*Singleton, error) {
    initOnce.Do(func() {
        instance, initErr = loadFromConfig()
    })
    return instance, initErr
}
```

## RWMutex for Read-Heavy Workloads

```go
type Cache struct {
    mu    sync.RWMutex
    items map[string]string
}

func (c *Cache) Get(key string) (string, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    val, ok := c.items[key]
    return val, ok
}

func (c *Cache) Set(key, val string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.items[key] = val
}
```

## atomic for Counters

```go
var (
    successCount atomic.Int64
    failCount    atomic.Int64
)

func recordResult(ok bool) {
    if ok {
        successCount.Add(1)
    } else {
        failCount.Add(1)
    }
}
```

## Channel Patterns

```go
// Pipeline: stages connected by channels
func Pipeline(ctx context.Context, input []Item) (<-chan Result, func()) {
    stage1 := make(chan Item, len(input))
    for _, item := range input {
        stage1 <- item
    }
    close(stage1)

    stage2 := make(chan Processed, len(input))
    var wg sync.WaitGroup
    ctx, cancel := context.WithCancel(ctx)

    for range 4 {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for item := range stage1 {
                select {
                case stage2 <- Process(ctx, item):
                case <-ctx.Done():
                    return
                }
            }
        }()
    }

    go func() {
        wg.Wait()
        close(stage2)
    }()

    return stage2, cancel
}
```
