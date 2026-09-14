package mysql

import (
	"GoAI/internal/testutil"
	"GoAI/model"
	"testing"
)

func TestMedicalEventMigration(t *testing.T) {
	db := testutil.MySQL(t)
	old := DB
	DB = db
	t.Cleanup(func() { DB = old })
	if err := db.AutoMigrate(&model.MedicalRun{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE medical_runs ADD COLUMN events MEDIUMTEXT").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migration(); err != nil {
			t.Fatal(err)
		}
	}
	if db.Migrator().HasColumn(&model.MedicalRun{}, "events") {
		t.Fatal("legacy column remains")
	}
	if !db.Migrator().HasTable(&model.MedicalRunEvent{}) || !db.Migrator().HasIndex(&model.MedicalRunEvent{}, "idx_medical_event_sequence") {
		t.Fatal("event table or unique index missing")
	}
}
