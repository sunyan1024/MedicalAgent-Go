package code

//code响应状态码

type Code int64

const (
	CodeSuccess Code = 1000

	CodeInvalidParams    Code = 2001
	CodeUserExist        Code = 2002
	CodeUserNotExist     Code = 2003
	CodeInvalidPassword  Code = 2004
	CodeNotMatchPassword Code = 2005
	CodeInvalidToken     Code = 2006
	CodeNotLogin         Code = 2007
	CodeInvalidCaptcha   Code = 2008
	CodeRecordNotFound   Code = 2009
	CodeIllegalPassword  Code = 2010

	CodeForbidden Code = 3001

	CodeServerBusy Code = 4001

	AIModelNotFind    Code = 5001
	AIModelCannotOpen Code = 5002
	AIModelFail       Code = 5003

	TTSFail Code = 6001
	ASRFail Code = 6002
)

// msg 定义了系统状态码与对应消息的映射关系
// 包含以下状态码分类：
// - 成功状态码 (CodeSuccess)
// - 客户端错误状态码 (CodeInvalidParams 等)
// - 权限相关状态码 (CodeForbidden)
// - 服务端错误状态码 (CodeServerBusy)
// - AI模型相关错误状态码 (AIModelNotFind 等)
var msg = map[Code]string{
	CodeSuccess: "success",

	CodeInvalidParams:    "请求参数错误",
	CodeUserExist:        "UID 或邮箱已存在",
	CodeUserNotExist:     "用户不存在",
	CodeInvalidPassword:  "UID、邮箱或密码错误",
	CodeNotMatchPassword: "两次密码不一致",
	CodeInvalidToken:     "无效的Token",
	CodeNotLogin:         "用户未登录",
	CodeInvalidCaptcha:   "验证码错误",
	CodeRecordNotFound:   "记录不存在",
	CodeIllegalPassword:  "密码不合法",

	CodeForbidden: "权限不足",

	CodeServerBusy: "服务繁忙",

	AIModelNotFind:    "模型不存在",
	AIModelCannotOpen: "无法打开模型",
	AIModelFail:       "模型运行失败",
	TTSFail:           "语音服务失败",
	ASRFail:           "语音识别失败",
}

// Code 返回该错误码的整型值
func (code Code) Code() int64 {
	return int64(code)
}

// Msg 获取响应消息
func (code Code) Msg() string {
	if m, ok := msg[code]; ok {
		return m
	}
	return msg[CodeServerBusy]
}
