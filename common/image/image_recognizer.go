package image

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/image/draw"
)

type ImageRecognizer struct {
	session      *ort.Session[float32] // ONNX运行时会话
	inputName    string                // 输入张量名称
	outputName   string                // 输出张量名称
	inputH       int                   // 输入图像高度
	inputW       int                   // 输入图像宽度
	labels       []string              // 分类标签列表
	inputTensor  *ort.Tensor[float32]  // 输入张量
	outputTensor *ort.Tensor[float32]  // 输出张量
}

const (
	defaultInputName  = "data"
	defaultOutputName = "mobilenetv20_output_flatten0_reshape0"
)

// 全局单次初始化
var (
	initOnce sync.Once
	initErr  error
)

// NewImageRecognizer 创建识别器（自动使用默认 input/output 名称）
func NewImageRecognizer(modelPath, labelPath string, inputH, inputW int) (*ImageRecognizer, error) {
	if inputH <= 0 || inputW <= 0 {
		inputH, inputW = 224, 224
	}

	// 初始化 ONNX 环境（全局一次）
	initOnce.Do(func() {
		initErr = ort.InitializeEnvironment()
	})
	if initErr != nil {
		return nil, fmt.Errorf("onnxruntime initialize error: %w", initErr)
	}

	// 预先创建输入输出 Tensor，避免每次预测时重新分配内存，提高性能
	inputShape := ort.NewShape(1, 3, int64(inputH), int64(inputW))
	inData := make([]float32, inputShape.FlattenedSize())
	inTensor, err := ort.NewTensor(inputShape, inData)
	if err != nil {
		return nil, fmt.Errorf("create input tensor failed: %w", err)
	}

	outShape := ort.NewShape(1, 1000)
	outTensor, err := ort.NewEmptyTensor[float32](outShape)
	if err != nil {
		inTensor.Destroy()
		return nil, fmt.Errorf("create output tensor failed: %w", err)
	}

	// 创建 Session
	session, err := ort.NewSession[float32](
		modelPath,
		[]string{defaultInputName},
		[]string{defaultOutputName},
		[]*ort.Tensor[float32]{inTensor},
		[]*ort.Tensor[float32]{outTensor},
	)
	if err != nil {
		inTensor.Destroy()
		outTensor.Destroy()
		return nil, fmt.Errorf("create onnx session failed: %w", err)
	}

	// 读取 label 文件
	labels, err := loadLabels(labelPath)
	if err != nil {
		session.Destroy()
		inTensor.Destroy()
		outTensor.Destroy()
		return nil, err
	}

	return &ImageRecognizer{
		session:      session,
		inputName:    defaultInputName,
		outputName:   defaultOutputName,
		inputH:       inputH,
		inputW:       inputW,
		labels:       labels,
		inputTensor:  inTensor,
		outputTensor: outTensor,
	}, nil
}

// 释放ONNX资源
func (r *ImageRecognizer) Close() {
	if r.session != nil {
		_ = r.session.Destroy()
		r.session = nil
	}
	if r.inputTensor != nil {
		_ = r.inputTensor.Destroy()
		r.inputTensor = nil
	}
	if r.outputTensor != nil {
		_ = r.outputTensor.Destroy()
		r.outputTensor = nil
	}
}

// 从文件路径读取图像
func (r *ImageRecognizer) PredictFromFile(imagePath string) (string, error) {
	// 读取图像文件
	file, err := os.Open(filepath.Clean(imagePath))
	if err != nil {
		return "", fmt.Errorf("image not found: %w", err)
	}
	defer file.Close()

	// 从文件解码图像
	img, _, err := image.Decode(file)
	if err != nil {
		return "", fmt.Errorf("failed to decode image: %w", err)
	}

	return r.PredictFromImage(img) // 从已解码的image.Image对象预测
}

// 从字节数据读取图像
func (r *ImageRecognizer) PredictFromBuffer(buf []byte) (string, error) {
	// 从字节流解码图像
	img, _, err := image.Decode(bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("failed to decode image from buffer: %w", err)
	}
	return r.PredictFromImage(img) // 从已解码的image.Image对象预测
}

// 从已解码的image.Image对象预测
func (r *ImageRecognizer) PredictFromImage(img image.Image) (string, error) {
	// 尺寸调整 使用CatmullRom算法进行高质量图像缩放，将输入图像调整为模型需要的尺寸(默认224x224)
	resizedImg := image.NewRGBA(image.Rect(0, 0, r.inputW, r.inputH))
	draw.CatmullRom.Scale(resizedImg, resizedImg.Bounds(), img, img.Bounds(), draw.Over, nil)

	// 数据格式转换
	h, w := r.inputH, r.inputW
	ch := 3 // R, G, B
	data := make([]float32, h*w*ch)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := resizedImg.At(x, y)

			r, g, b, _ := c.RGBA()
			// 将RGBA值转换为0-1范围的浮点数，并进行标准化
			// 使用位移操作( r>>8 )高效提取颜色分量
			rf := (float32(r>>8)/255.0 - 0.485) / 0.229 // 使用ImageNet标准化参数
			gf := (float32(g>>8)/255.0 - 0.456) / 0.224
			bf := (float32(b>>8)/255.0 - 0.406) / 0.225

			// NCHW format 按照NCHW格式（批次数-通道数-高度-宽度）组织数据
			data[y*w+x] = rf
			data[h*w+y*w+x] = gf
			data[2*h*w+y*w+x] = bf
		}
	}

	inData := r.inputTensor.GetData()
	copy(inData, data)

	// 模型推理
	if err := r.session.Run(); err != nil {
		return "", fmt.Errorf("onnx run error: %w", err)
	}

	// 结果处理
	outData := r.outputTensor.GetData()
	if len(outData) == 0 {
		return "", errors.New("empty output from model")
	}

	maxIdx := 0          // 最大激活值的索引
	maxVal := outData[0] // 最大激活值
	for i := 1; i < len(outData); i++ {
		if outData[i] > maxVal {
			maxVal = outData[i]
			maxIdx = i
		}
	}

	if maxIdx >= 0 && maxIdx < len(r.labels) {
		return r.labels[maxIdx], nil // 返回最大值对应的分类标签
	}
	return "Unknown", nil
}

// loadLabels 从指定路径读取标签文件，每行作为一个标签返回
// 参数 path: 标签文件路径
// 返回: 标签字符串切片和可能的错误
// 错误情况: 文件打开失败、读取错误或文件为空
func loadLabels(path string) ([]string, error) {
	f, err := os.Open(filepath.Clean(path)) // 使用 filepath.Clean(path) 清理路径，防止路径遍历攻击
	if err != nil {
		return nil, fmt.Errorf("open label file failed: %w", err)
	}
	defer f.Close() // 避免资源泄露

	var labels []string
	sc := bufio.NewScanner(f) // 使用 bufio.Scanner 进行高效的逐行读取，适合大文件
	for sc.Scan() {
		line := sc.Text()
		if line != "" {
			labels = append(labels, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read labels failed: %w", err)
	}
	if len(labels) == 0 {
		return nil, fmt.Errorf("no labels found in %s", path)
	}
	return labels, nil
}
