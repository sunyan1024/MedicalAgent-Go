package file

import (
	"GoAI/common/mysql"
	"GoAI/model"

	"gorm.io/gorm"
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

func CreateStoredFile(storedFile *model.StoredFile) (*model.StoredFile, error) {
	err := mysql.DB.Create(storedFile).Error
	return storedFile, err
}

func GetStoredFileBySHA256AndExt(fileSHA256 string, ext string) (*model.StoredFile, error) {
	var storedFile model.StoredFile
	err := mysql.DB.Where("file_sha256 = ? AND ext = ?", fileSHA256, ext).First(&storedFile).Error
	return &storedFile, err
}

func GetStoredFileByID(id string) (*model.StoredFile, error) {
	var storedFile model.StoredFile
	err := mysql.DB.Where("id = ?", id).First(&storedFile).Error
	return &storedFile, err
}

func IncrementStoredFileRefCount(tx *gorm.DB, storedFileID string) error {
	return tx.Model(&model.StoredFile{}).
		Where("id = ?", storedFileID).
		UpdateColumn("ref_count", gorm.Expr("ref_count + ?", 1)).Error
}

func DecrementStoredFileRefCount(tx *gorm.DB, storedFileID string) error {
	return tx.Model(&model.StoredFile{}).
		Where("id = ? AND ref_count > 0", storedFileID).
		UpdateColumn("ref_count", gorm.Expr("ref_count - ?", 1)).Error
}

func DeleteStoredFileByID(tx *gorm.DB, storedFileID string) error {
	return tx.Where("id = ?", storedFileID).Delete(&model.StoredFile{}).Error
}
