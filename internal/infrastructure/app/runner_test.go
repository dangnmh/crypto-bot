package app_test

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/app"

	"github.com/stretchr/testify/assert"
)

type mockBot struct {
	runAsBackgroundErr error
	runErr             error
	stopErr            error
	runStarted         atomic.Bool
	runStopped         atomic.Bool
}

func (m *mockBot) RunAsBackground(_ context.Context) error {
	return m.runAsBackgroundErr
}

func (m *mockBot) Run(ctx context.Context) error {
	m.runStarted.Store(true)
	<-ctx.Done()
	m.runStopped.Store(true)
	return m.runErr
}

func (m *mockBot) Stop(_ context.Context) error {
	return m.stopErr
}

func TestRunBot_Lifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping signal test on Windows — process.Signal() not supported")
	}
	t.Parallel()
	bot := &mockBot{}
	engine := &app.Engine{Providers: map[string]*app.ExchangeProvider{}}

	done := make(chan struct{})
	go func() {
		err := app.RunBot(engine, bot)
		assert.NoError(t, err)
		close(done)
	}()

	// Allow app.RunBot to start Run()
	time.Sleep(50 * time.Millisecond)

	pid := os.Getpid()
	process, err := os.FindProcess(pid)
	assert.NoError(t, err)
	err = process.Signal(os.Interrupt)
	assert.NoError(t, err)

	select {
	case <-done:
		assert.True(t, bot.runStarted.Load())
		assert.True(t, bot.runStopped.Load())
	case <-time.After(2 * time.Second):
		t.Fatal("app.RunBot did not exit after signal")
	}
}

func TestRunBot_BackgroundFail(t *testing.T) {
	t.Parallel()
	bot := &mockBot{
		runAsBackgroundErr: fmt.Errorf("background fail"),
	}
	engine := &app.Engine{Providers: map[string]*app.ExchangeProvider{}}

	err := app.RunBot(engine, bot)
	assert.Error(t, err)
	assert.Equal(t, "background fail", err.Error())
	assert.False(t, bot.runStarted.Load())
}

func TestRunBot_StopError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping signal test on Windows — process.Signal() not supported")
	}
	t.Parallel()
	bot := &mockBot{
		stopErr: fmt.Errorf("stop failed"),
	}
	engine := &app.Engine{Providers: map[string]*app.ExchangeProvider{}}

	done := make(chan struct{})
	go func() {
		_ = app.RunBot(engine, bot)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	pid := os.Getpid()
	process, _ := os.FindProcess(pid)
	_ = process.Signal(os.Interrupt)

	select {
	case <-done:
		assert.True(t, bot.runStarted.Load())
	case <-time.After(2 * time.Second):
		t.Fatal("app.RunBot did not exit")
	}
}
