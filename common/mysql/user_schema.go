package mysql

import (
	"GoAI/model"
	"fmt"
)

// prepareUIDSchema replaces only empty legacy user tables and renames ownership columns.
// AutoMigrate alone does not remove the old auto-increment primary key.
func prepareUIDSchema() error {
	m := DB.Migrator()
	if m.HasTable(&model.User{}) && (m.HasColumn(&model.User{}, "id") || m.HasColumn(&model.User{}, "username") || m.HasColumn(&model.User{}, "name")) {
		var count int64
		if err := DB.Table("users").Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("users 仍有旧数据，请先清空旧用户数据再启用 UID 表结构")
		}
		if err := m.DropTable(&model.User{}); err != nil {
			return err
		}
	}
	for _, table := range []interface{}{&model.Session{}, &model.Message{}, &model.KnowledgeFile{}, &model.MedicalReport{}, &model.MedicalRun{}} {
		if m.HasTable(table) && m.HasColumn(table, "user_name") && !m.HasColumn(table, "uid") {
			if err := m.RenameColumn(table, "user_name", "uid"); err != nil {
				return err
			}
		}
	}
	return nil
}
