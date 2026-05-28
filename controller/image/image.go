package image

import (
	"GoAI/common/code"
	"GoAI/controller"
	"GoAI/service/image"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type (
	// RecognizeImageResponse 表示图像识别API的响应结构
	// ClassName: AI识别结果的类别名称
	// Response: 嵌入的基础响应结构体，包含状态码等信息
	RecognizeImageResponse struct {
		ClassName           string `json:"class_name,omitempty"` // AI识别结果
		controller.Response        // 嵌入基础响应结构体
	}
)

// 处理图像识别请求
func RecognizeImage(c *gin.Context) {
	res := new(RecognizeImageResponse) // 用于返回结果

	file, err := c.FormFile("image") //从表单数据中获取名为"image"的文件
	if err != nil {
		log.Println("[RecognizeImage] FormFile fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	className, err := image.RecognizeImage(file) // 将文件传递给服务层进行实际的AI识别
	if err != nil {
		log.Println("[RecognizeImage] fail ", err)
		c.JSON(http.StatusOK, res.CodeOf(code.CodeServerBusy))
		return
	}

	//返回成功响应
	res.Success()             //标记响应为成功状态
	res.ClassName = className //设置识别结果

	c.JSON(http.StatusOK, res) //返回JSON格式的响应
}
