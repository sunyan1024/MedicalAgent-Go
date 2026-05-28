package image

import (
	"GoAI/common/image"
	"io"
	"log"
	"mime/multipart"
)

// RecognizeImage 使用预训练的MobileNetV2模型识别上传图像的内容
//
// 参数:
//
//	file: 包含要识别图像的多部分文件头
//
// 返回值:
//
//	string: 识别出的图像类别名称
//	error: 识别过程中发生的任何错误
func RecognizeImage(file *multipart.FileHeader) (string, error) {
	modelPath := "cnn_models/mobilenetv2/mobilenetv2-7.onnx"
	labelPath := "cnn_models/imagenet_classes.txt"
	inputH, inputW := 224, 224

	// 创建图像识别器
	recognizer, err := image.NewImageRecognizer(modelPath, labelPath, inputH, inputW)
	if err != nil {
		log.Println("NewImageRecognizer fail err is : ", err)
		return "", err
	}
	defer recognizer.Close()

	// 打开文件
	src, err := file.Open()
	if err != nil {
		log.Println("file open fail err is : ", err)
		return "", err
	}
	defer src.Close()

	// 读取文件内容
	buf, err := io.ReadAll(src)
	if err != nil {
		log.Println("io.ReadAll fail err is : ", err)
		return "", err
	}

	return recognizer.PredictFromBuffer(buf) // 从文件内容中识别图像
}
