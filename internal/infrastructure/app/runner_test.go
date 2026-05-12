package app_test

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"

	"crypto-bot/internal/infrastructure/app"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
)

type mockClient struct {
	exchange.Client
}

type mockBot struct {
	runAsBackgroundErr error
	runErr             error
	stopErr            error
	runStarted         bool
	runStopped         bool
}

func (m *mockBot) RunAsBackground(_ context.Context) error {
	return m.runAsBackgroundErr
}

func (m *mockBot) Run(ctx context.Context) error {
	m.runStarted = true
	<-ctx.Done()
	m.runStopped = true
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
	engine := app.NewEngine(app.EngineConfig{
		SystemConfig: &config.SystemConfig{},
		Client:       &mockClient{},
	})

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
		assert.True(t, bot.runStarted)
		assert.True(t, bot.runStopped)
	case <-time.After(2 * time.Second):
		t.Fatal("app.RunBot did not exit after signal")
	}
}

func TestRunBot_BackgroundFail(t *testing.T) {
	t.Parallel()
	bot := &mockBot{
		runAsBackgroundErr: fmt.Errorf("background fail"),
	}
	engine := app.NewEngine(app.EngineConfig{
		SystemConfig: &config.SystemConfig{},
		Client:       &mockClient{},
	})

	err := app.RunBot(engine, bot)
	assert.Error(t, err)
	assert.Equal(t, "background fail", err.Error())
	assert.False(t, bot.runStarted)
}

func TestRunBot_StopError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping signal test on Windows — process.Signal() not supported")
	}
	t.Parallel()
	bot := &mockBot{
		stopErr: fmt.Errorf("stop failed"),
	}
	engine := app.NewEngine(app.EngineConfig{
		SystemConfig: &config.SystemConfig{},
		Client:       &mockClient{},
	})

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
		assert.True(t, bot.runStarted)
	case <-time.After(2 * time.Second):
		t.Fatal("app.RunBot did not exit")
	}
}
