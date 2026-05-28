package file

import (
	"GoAI/common/mysql"
	"GoAI/model"
)

func CreateKnowledgeFile(file *model.KnowledgeFile) (*model.KnowledgeFile, error) {
	err := mysql.DB.Create(file).Error
	return file, err
}

func GetKnowledgeFilesByUserName(userName string) ([]model.KnowledgeFile, error) {
	var files []model.KnowledgeFile
	err := mysql.DB.Where("user_name = ?", userName).Order("created_at desc").Find(&files).Error
	return files, err
}

func GetKnowledgeFileByID(userName string, fileID string) (*model.KnowledgeFile, error) {
	var file model.KnowledgeFile
	err := mysql.DB.Where("user_name = ? AND id = ?", userName, fileID).First(&file).Error
	return &file, err
}

func GetKnowledgeFileByUserNameAndStoredName(userName string, storedName string) (*model.KnowledgeFile, error) {
	var file model.KnowledgeFile
	err := mysql.DB.Where("user_name = ? AND stored_name = ?", userName, storedName).First(&file).Error
	return &file, err
}

func DeleteKnowledgeFileByID(userName string, fileID string) error {
	return mysql.DB.Where("user_name = ? AND id = ?", userName, fileID).Delete(&model.KnowledgeFile{}).Error
}
