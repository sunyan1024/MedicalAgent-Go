package testutil

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStack starts a disposable server on a free loopback port with persistence disabled.
// Tests never change the application Redis configuration or depend on its write health.
func RedisStack(t *testing.T) *redis.Client {
	t.Helper()
	server := os.Getenv("GOPHERAI_TEST_REDIS_SERVER")
	if server == "" {
		server = "/opt/redis-stack/bin/redis-server"
	}
	module := os.Getenv("GOPHERAI_TEST_REDISEARCH_MODULE")
	if module == "" {
		module = "/opt/redis-stack/lib/redisearch.so"
	}
	for _, path := range []string{server, module} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Redis Stack test dependency unavailable: %s; configure GOPHERAI_TEST_REDIS_SERVER and GOPHERAI_TEST_REDISEARCH_MODULE", path)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "redis.log")
	output, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { output.Close() })
	cmd := exec.Command(server, "--bind", "127.0.0.1", "--port", fmt.Sprint(port), "--save", "", "--appendonly", "no", "--dir", dir, "--loadmodule", module)
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("127.0.0.1:%d", port), Protocol: 2, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, MaxRetries: -1})
	t.Cleanup(func() { client.Close() })
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if client.Ping(context.Background()).Err() == nil {
			return client
		}
		time.Sleep(50 * time.Millisecond)
	}
	logs, _ := os.ReadFile(logPath)
	t.Fatalf("isolated Redis Stack did not start: %s", logs)
	return nil
}
