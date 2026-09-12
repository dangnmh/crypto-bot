package ws_test

import (
	"sync"
	"testing"
	"time"

	"crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestDispatcher_RegisterAndDispatch(t *testing.T) {
	t.Parallel()

	d := ws.NewRequestDispatcher[string]()
	ch, cancel, err := d.Register("req-1")
	require.NoError(t, err)
	defer cancel()

	assert.Equal(t, 1, d.Len())

	ok := d.Dispatch("req-1", "response-1")
	assert.True(t, ok)
	assert.Equal(t, 0, d.Len())

	select {
	case res, open := <-ch:
		require.True(t, open)
		assert.Equal(t, "response-1", res)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestRequestDispatcher_DispatchUnknownID(t *testing.T) {
	t.Parallel()

	d := ws.NewRequestDispatcher[string]()
	ok := d.Dispatch("unknown", "value")
	assert.False(t, ok)
}

func TestRequestDispatcher_DuplicateRegister(t *testing.T) {
	t.Parallel()

	d := ws.NewRequestDispatcher[string]()
	_, cancel1, err1 := d.Register("req-1")
	require.NoError(t, err1)
	defer cancel1()

	_, _, err2 := d.Register("req-1")
	assert.ErrorIs(t, err2, ws.ErrDuplicateRequestID)
}

func TestRequestDispatcher_CancelRemovesPending(t *testing.T) {
	t.Parallel()

	d := ws.NewRequestDispatcher[string]()
	ch, cancel, err := d.Register("req-1")
	require.NoError(t, err)

	assert.Equal(t, 1, d.Len())
	cancel()
	assert.Equal(t, 0, d.Len())

	// Dispatch after cancel should return false
	ok := d.Dispatch("req-1", "response-1")
	assert.False(t, ok)

	select {
	case <-ch:
		t.Fatal("unexpected message on cancelled channel")
	default:
	}
}

func TestRequestDispatcher_CloseCancelsAllPending(t *testing.T) {
	t.Parallel()

	d := ws.NewRequestDispatcher[string]()
	ch1, _, err1 := d.Register("req-1")
	require.NoError(t, err1)
	ch2, _, err2 := d.Register("req-2")
	require.NoError(t, err2)

	assert.Equal(t, 2, d.Len())
	d.Close()
	assert.Equal(t, 0, d.Len())

	// Both channels should be closed
	_, open1 := <-ch1
	assert.False(t, open1)
	_, open2 := <-ch2
	assert.False(t, open2)

	// New registrations after close should fail
	_, _, err3 := d.Register("req-3")
	assert.ErrorIs(t, err3, ws.ErrDispatcherClosed)
}

func TestRequestDispatcher_AbortAll(t *testing.T) {
	t.Parallel()

	d := ws.NewRequestDispatcher[string]()
	ch1, _, err1 := d.Register("req-1")
	require.NoError(t, err1)
	ch2, _, err2 := d.Register("req-2")
	require.NoError(t, err2)

	assert.Equal(t, 2, d.Len())
	d.AbortAll()
	assert.Equal(t, 0, d.Len())

	// Both channels should be closed
	_, open1 := <-ch1
	assert.False(t, open1)
	_, open2 := <-ch2
	assert.False(t, open2)

	// New registrations after AbortAll should STILL SUCCEED (unlike Close)
	ch3, cancel3, err3 := d.Register("req-3")
	require.NoError(t, err3)
	defer cancel3()
	assert.NotNil(t, ch3)
	assert.Equal(t, 1, d.Len())
}

func TestRequestDispatcher_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	d := ws.NewRequestDispatcher[int]()
	var wg sync.WaitGroup
	const total = 100

	// Concurrent register and dispatch
	for i := range total {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			reqID := time.Now().Format("150405.000000000") + string(rune('A'+(idx%26))) + string(rune('0'+(idx%10)))
			ch, cancel, err := d.Register(reqID)
			if err != nil {
				return
			}
			defer cancel()

			go func() {
				time.Sleep(2 * time.Millisecond)
				d.Dispatch(reqID, idx)
			}()

			select {
			case val := <-ch:
				assert.Equal(t, idx, val)
			case <-time.After(500 * time.Millisecond):
				t.Errorf("timeout waiting for %s", reqID)
			}
		}(i)
	}

	wg.Wait()
}
