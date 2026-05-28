package file

import (
	"GoAI/common/code"
	"GoAI/controller"
	"GoAI/model"
	"GoAI/service/file"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type (
	// UploadFileResponse 表示文件上传成功的响应结构
	// 包含文件路径和基础响应信息
	UploadFileResponse struct {
		File *model.KnowledgeFile `json:"file,omitempty"`
		controller.Response
	}

	PrecheckRequest struct {
		OriginalName string `json:"original_name"`
		FileSHA256   string `json:"file_sha256"`
		Size         int64  `json:"size"`
	}

	PrecheckResponse struct {
		NeedUpload bool                 `json:"need_upload"`
		Reused     bool                 `json:"reused"`
		File       *model.KnowledgeFile `json:"file,omitempty"`
		controller.Response
	}

	ListFileResponse struct {
		Files []model.KnowledgeFile `json:"files"`
		controller.Response
	}
)

func PrecheckRagFile(c *gin.Context) {
	res := new(PrecheckResponse)

	username := c.GetString("userName")
	if username == "" {
		log.Println("[PrecheckRagFile] Username not found in context")
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
		return
	}

	var req PrecheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Println("[PrecheckRagFile] bind fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	result, err := file.PrecheckRagFile(username, req.OriginalName, req.FileSHA256, req.Size)
	if err != nil {
		log.Println("[PrecheckRagFile] fail ", err)
		res.CodeOf(code.CodeInvalidParams)
		res.StatusMsg = err.Error()
		c.JSON(http.StatusOK, res)
		return
	}

	res.Success()
	res.NeedUpload = result.NeedUpload
	res.Reused = result.Reused
	res.File = result.File
	c.JSON(http.StatusOK, res)
}

func UploadRagFile(c *gin.Context) {
	res := new(UploadFileResponse)

	// 从表单数据中获取名为"file"的文件
	uploadedFile, err := c.FormFile("file")
	if err != nil {
		log.Println("[UploadRagFile] FormFile fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	// 从上下文中获取用户名
	username := c.GetString("userName")
	if username == "" {
		log.Println("[UploadRagFile] Username not found in context")
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
		return
	}

	//indexer 会在 service 层根据实际文件名创建
	fileSHA256 := c.PostForm("file_sha256")
	knowledgeFile, err := file.UploadRagFile(username, uploadedFile, fileSHA256)
	if err != nil {
		log.Println("[UploadRagFile] fail ", err)
		if strings.Contains(err.Error(), "文件类型不正确") ||
			strings.Contains(err.Error(), "SHA-256") ||
			errors.Is(err, file.ErrDuplicateFile) ||
			errors.Is(err, file.ErrHashMismatch) {
			res.CodeOf(code.CodeInvalidParams)
			res.StatusMsg = err.Error()
			c.JSON(http.StatusOK, res)
			return
		}

		res.CodeOf(code.CodeServerBusy)
		res.StatusMsg = err.Error()
		c.JSON(http.StatusOK, res)
		return
	}

	res.Success()
	res.File = knowledgeFile

	c.JSON(http.StatusOK, res)
}

func ListKnowledgeFiles(c *gin.Context) {
	res := new(ListFileResponse)

	username := c.GetString("userName")
	if username == "" {
		log.Println("[ListKnowledgeFiles] Username not found in context")
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
		return
	}

	files, err := file.ListKnowledgeFiles(username)
	if err != nil {
		log.Println("[ListKnowledgeFiles] fail ", err)
		res.CodeOf(code.CodeServerBusy)
		res.StatusMsg = err.Error()
		c.JSON(http.StatusOK, res)
		return
	}

	res.Success()
	res.Files = files
	c.JSON(http.StatusOK, res)
}

func DeleteKnowledgeFile(c *gin.Context) {
	res := new(controller.Response)

	username := c.GetString("userName")
	if username == "" {
		log.Println("[DeleteKnowledgeFile] Username not found in context")
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
		return
	}

	fileID := c.Param("id")
	if fileID == "" {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	if err := file.DeleteKnowledgeFile(username, fileID); err != nil {
		log.Println("[DeleteKnowledgeFile] fail ", err)
		res.CodeOf(code.CodeServerBusy)
		res.StatusMsg = err.Error()
		c.JSON(http.StatusOK, res)
		return
	}

	res.Success()
	c.JSON(http.StatusOK, res)
}
