// Test fixture copied unchanged into each detached worktree; no production edits.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"GoAI/common/aihelper"
	appdb "GoAI/common/mysql"
	"GoAI/common/rabbitmq"
	"GoAI/common/redis"
	"GoAI/config"
	"GoAI/model"
	"GoAI/router"
	"GoAI/utils"
	"github.com/BurntSushi/toml"
	"github.com/gin-gonic/gin"
	driver "github.com/go-sql-driver/mysql"
	"github.com/streadway/amqp"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func save(name string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	must(e)
	must(os.WriteFile(filepath.Join(os.Getenv("COMPARE_OUT"), name), b, 0600))
}
func hash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type capture struct {
	base            http.RoundTripper
	mu              sync.Mutex
	count, failures int
}
type tap struct {
	io.ReadCloser
	buf    bytes.Buffer
	once   sync.Once
	finish func([]byte)
}

func (t *tap) Read(p []byte) (int, error) {
	n, e := t.ReadCloser.Read(p)
	t.buf.Write(p[:n])
	if e != nil {
		t.once.Do(func() { t.finish(t.buf.Bytes()) })
	}
	return n, e
}
func (t *tap) Close() error {
	e := t.ReadCloser.Close()
	t.once.Do(func() { t.finish(t.buf.Bytes()) })
	return e
}
func (t *capture) RoundTrip(r *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		return t.base.RoundTrip(r)
	}
	t.mu.Lock()
	if t.count >= 40 || t.failures >= 3 {
		t.mu.Unlock()
		return nil, fmt.Errorf("comparison stop: HTTP budget or consecutive provider errors")
	}
	t.count++
	n := t.count
	t.mu.Unlock()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		return nil, e
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	var body map[string]any
	must(json.Unmarshal(b, &body))
	save(fmt.Sprintf("model-%03d-request.json", n), body)
	record := map[string]any{"number": n, "round": os.Getenv("unused"), "messages_hash": hash(body["messages"]), "tools_hash": hash(body["tools"]), "started_at": time.Now().UTC(), "path": r.URL.Path}
	started := time.Now()
	res, e := t.base.RoundTrip(r)
	if e != nil {
		t.mu.Lock()
		t.failures++
		t.mu.Unlock()
		record["error"] = "transport failure"
		save(fmt.Sprintf("model-%03d-meta.json", n), record)
		return nil, e
	}
	record["status"] = res.StatusCode
	record["request_id"] = res.Header.Get("X-Request-Id")
	t.mu.Lock()
	if res.StatusCode >= 400 {
		t.failures++
	} else {
		t.failures = 0
	}
	t.mu.Unlock()
	res.Body = &tap{ReadCloser: res.Body, finish: func(b []byte) {
		// Response payload contains only synthetic model output, never HTTP headers.
		must(os.WriteFile(filepath.Join(os.Getenv("COMPARE_OUT"), fmt.Sprintf("model-%03d-response.txt", n)), b, 0600))
		usage := []any{}
		ids := []string{}
		parse := func(b []byte) {
			var x map[string]any
			if json.Unmarshal(b, &x) == nil {
				if u := x["usage"]; u != nil {
					usage = append(usage, u)
				}
				if id, ok := x["id"].(string); ok {
					if len(ids) == 0 || ids[len(ids)-1] != id {
						ids = append(ids, id)
					}
				}
			}
		}
		if json.Valid(b) {
			parse(b)
		} else {
			for _, line := range bytes.Split(b, []byte("\n")) {
				if bytes.HasPrefix(line, []byte("data:")) {
					parse(bytes.TrimSpace(line[5:]))
				}
			}
		}
		record["raw_usage"] = usage
		record["response_ids"] = ids
		record["duration_ms"] = float64(time.Since(started).Microseconds()) / 1000
		save(fmt.Sprintf("model-%03d-meta.json", n), record)
	}}
	return res, nil
}
func main() {
	// Production log paths can print token fragments: discard them before any init.
	log.SetOutput(io.Discard)
	gin.SetMode(gin.ReleaseMode)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	protocol := os.Stdout
	null, e := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	must(e)
	os.Stdout = null
	os.Stderr = null
	c := config.GetConfig()
	_, e = toml.DecodeFile(os.Getenv("COMPARE_CONFIG"), c)
	must(e)
	name := os.Getenv("COMPARE_DB")
	queue := os.Getenv("COMPARE_QUEUE")
	if !strings.HasPrefix(name, "httpcmp_") || strings.ContainsAny(name, "`/\\") || !strings.HasPrefix(queue, "httpcmp_") {
		panic("invalid isolated resource name")
	}
	d := driver.NewConfig()
	d.User = c.MysqlUser
	d.Passwd = c.MysqlPassword
	d.Net = "tcp"
	d.Addr = fmt.Sprintf("%s:%d", c.MysqlHost, c.MysqlPort)
	db, e := sql.Open("mysql", d.FormatDSN())
	must(e)
	defer db.Close()
	if os.Getenv("COMPARE_MODE") == "cleanup" {
		// Server process must have exited before this mode is invoked.
		url := fmt.Sprintf("amqp://%s:%s@%s:%d/%s", c.RabbitmqUsername, c.RabbitmqPassword, c.RabbitmqHost, c.RabbitmqPort, c.RabbitmqVhost)
		conn, e := amqp.Dial(url)
		must(e)
		ch, e := conn.Channel()
		must(e)
		_, e = ch.QueueDelete(queue, false, false, false)
		must(e)
		ch.Close()
		conn.Close()
		_, e = db.Exec("DROP DATABASE IF EXISTS `" + name + "`")
		must(e)
		fmt.Fprintln(protocol, `{"cleaned":true}`)
		return
	}
	_, e = db.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4")
	must(e)
	c.MysqlDatabaseName = name
	must(appdb.InitMysql())
	must(redis.Init())
	http.DefaultTransport = &capture{base: http.DefaultTransport}
	rabbitmq.RMQMessage = rabbitmq.NewWorkRabbitMQ(queue)
	go rabbitmq.RMQMessage.Consume(rabbitmq.MQMessage)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	must(e)
	srv := &http.Server{Handler: router.InitRouter(), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(listener)
	fmt.Fprintf(protocol, "{\"url\":\"http://%s/api/v3\",\"model\":%q,\"thinking\":%t}\n", listener.Addr(), c.QwenModelName, c.QwenConfig.EnableThinking)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req struct{ Op, UID, Password, Session string }
		must(json.Unmarshal(scanner.Bytes(), &req))
		switch req.Op {
		case "seed":
			pw, e := utils.GenerateFromPassword(req.Password)
			must(e)
			must(appdb.DB.Create(&model.User{UID: req.UID, Role: model.RoleUser, Email: req.UID + "@synthetic.invalid", Nickname: "HTTP测试", Password: string(pw)}).Error)
			fmt.Fprintln(protocol, `{"seeded":true}`)
		case "snapshot":
			var msgs []model.Message
			must(appdb.DB.Where("uid = ? AND session_id = ?", req.UID, req.Session).Order("message_id").Find(&msgs).Error)
			var runs []model.MedicalRun
			must(appdb.DB.Where("uid = ? AND session_id = ?", req.UID, req.Session).Find(&runs).Error)
			var helper []*model.Message
			if h, ok := aihelper.GetGlobalManager().GetAIHelper(req.UID, req.Session); ok {
				helper = h.GetMessages()
			}
			b, _ := json.Marshal(map[string]any{"database": msgs, "helper": helper, "runs": runs})
			fmt.Fprintln(protocol, string(b))
		case "stop":
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			srv.Shutdown(ctx)
			cancel()
			rabbitmq.DestroyRabbitMQ()
			fmt.Fprintln(protocol, `{"stopped":true}`)
			return
		}
	}
	srv.Close()
	rabbitmq.DestroyRabbitMQ()
}
