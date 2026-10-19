package rabbitmq

import (
	"encoding/json"
	"testing"

	"GoAI/common/mysql"
	"GoAI/internal/testutil"
	"GoAI/model"
	"github.com/streadway/amqp"
)

func TestDurationMQSerialization(t *testing.T) {
	duration := int64(1250)
	for _, want := range []int64{0, duration} {
		data := GenerateMessageMQParam("session", "answer", "alice", false, want)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil || fields["duration_ms"] == nil {
			t.Fatalf("duration_ms must be present, including zero: %s", data)
		}
		var decoded MessageMQParam
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.DurationMS != want {
			t.Fatalf("duration lost in MQ: %s", data)
		}
	}

}

func TestDurationMQConsumerPersistence(t *testing.T) {
	db := testutil.MySQL(t)
	previous := mysql.DB
	mysql.DB = db
	t.Cleanup(func() { mysql.DB = previous })
	if err := db.AutoMigrate(&model.Session{}, &model.Message{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Session{ID: "session", UID: "alice"}).Error; err != nil {
		t.Fatal(err)
	}
	duration := int64(1250)
	for _, want := range []int64{0, duration} {
		payload := GenerateMessageMQParam("session", "answer", "alice", false, want)
		if err := MQMessage(&amqp.Delivery{Body: payload}); err != nil {
			t.Fatal(err)
		}
	}
	var messages []model.Message
	if err := db.Order("message_id").Find(&messages).Error; err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].DurationMS != 0 || messages[1].DurationMS != duration {
		t.Fatalf("persisted durations changed: %+v", messages)
	}
}
