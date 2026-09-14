package file

import (
	"GoAI/common/mysql"
	"GoAI/model"
	"time"

	"gorm.io/gorm"
)

func CreateKnowledgeFile(file *model.KnowledgeFile) (*model.KnowledgeFile, error) {
	err := mysql.DB.Create(file).Error
	return file, err
}

func ownedFiles(db *gorm.DB, owner model.KnowledgeOwner) *gorm.DB {
	return db.Where("scope = ? AND owner_key = ?", owner.Scope, owner.Key)
}

func GetKnowledgeFiles(owner model.KnowledgeOwner) ([]model.KnowledgeFile, error) {
	files := make([]model.KnowledgeFile, 0)
	err := ownedFiles(mysql.DB, owner).Order("created_at desc").Find(&files).Error
	return files, err
}

func GetKnowledgeFileByID(owner model.KnowledgeOwner, fileID string) (*model.KnowledgeFile, error) {
	var file model.KnowledgeFile
	err := ownedFiles(mysql.DB, owner).Where("id = ?", fileID).First(&file).Error
	return &file, err
}

func GetKnowledgeFileByStoredName(owner model.KnowledgeOwner, storedName string) (*model.KnowledgeFile, error) {
	var file model.KnowledgeFile
	err := ownedFiles(mysql.DB, owner).Where("stored_name = ?", storedName).First(&file).Error
	return &file, err
}

func DeleteKnowledgeFile(tx *gorm.DB, owner model.KnowledgeOwner, fileID string) error {
	result := ownedFiles(tx, owner).Where("id = ?", fileID).Delete(&model.KnowledgeFile{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// The same authorized set feeds both vector and keyword retrieval.
func GetAccessibleKnowledgeFiles(uid string) ([]model.KnowledgeFile, error) {
	files := make([]model.KnowledgeFile, 0)
	err := mysql.DB.Where("(scope = ? AND owner_key = ?) OR (scope = ? AND owner_key = ?)",
		model.KnowledgeScopePersonal, uid, model.KnowledgeScopeEnterprise, model.EnterpriseOwnerKey).
		Order("created_at desc").Find(&files).Error
	return files, err
}

func CreateStoredFile(storedFile *model.StoredFile) (*model.StoredFile, error) {
	err := mysql.DB.Create(storedFile).Error
	return storedFile, err
}

func GetStoredFileBySHA256AndExt(fileSHA256 string, ext string) (*model.StoredFile, error) {
	var storedFile model.StoredFile
	err := mysql.DB.Unscoped().Where("file_sha256 = ? AND ext = ?", fileSHA256, ext).First(&storedFile).Error
	return &storedFile, err
}

func GetStoredFileByID(id string) (*model.StoredFile, error) {
	var storedFile model.StoredFile
	err := mysql.DB.Where("id = ?", id).First(&storedFile).Error
	return &storedFile, err
}

func GetStoredFileByIDUnscoped(id string) (*model.StoredFile, error) {
	var storedFile model.StoredFile
	err := mysql.DB.Unscoped().Where("id = ?", id).First(&storedFile).Error
	return &storedFile, err
}

func UpdateStoredFileIndexPending(storedFileID string) error {
	return mysql.DB.Unscoped().Model(&model.StoredFile{}).
		Where("id = ?", storedFileID).
		Updates(map[string]interface{}{
			"index_status": model.StoredFileIndexStatusPending,
			"index_error":  "",
			"indexed_at":   nil,
		}).Error
}

func UpdateStoredFileIndexReady(storedFileID string, indexedAt time.Time) error {
	return mysql.DB.Unscoped().Model(&model.StoredFile{}).
		Where("id = ?", storedFileID).
		Updates(map[string]interface{}{
			"index_status": model.StoredFileIndexStatusReady,
			"index_error":  "",
			"indexed_at":   indexedAt,
		}).Error
}

func UpdateStoredFileIndexFailed(storedFileID string, indexError string) error {
	return mysql.DB.Unscoped().Model(&model.StoredFile{}).
		Where("id = ?", storedFileID).
		Updates(map[string]interface{}{
			"index_status": model.StoredFileIndexStatusFailed,
			"index_error":  indexError,
			"indexed_at":   nil,
		}).Error
}

func IncrementStoredFileRefCount(tx *gorm.DB, storedFileID string) error {
	return tx.Unscoped().Model(&model.StoredFile{}).
		Where("id = ?", storedFileID).
		Updates(map[string]interface{}{
			"ref_count":  gorm.Expr("ref_count + ?", 1),
			"deleted_at": nil,
		}).Error
}

func DecrementStoredFileRefCount(tx *gorm.DB, storedFileID string) error {
	return tx.Model(&model.StoredFile{}).
		Where("id = ? AND ref_count > 0", storedFileID).
		UpdateColumn("ref_count", gorm.Expr("ref_count - ?", 1)).Error
}

func DeleteStoredFileByID(tx *gorm.DB, storedFileID string) error {
	return tx.Where("id = ?", storedFileID).Delete(&model.StoredFile{}).Error
}

func HardDeleteStoredFileByID(tx *gorm.DB, storedFileID string) error {
	return tx.Unscoped().Where("id = ?", storedFileID).Delete(&model.StoredFile{}).Error
}
