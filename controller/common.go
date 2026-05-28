package controller

import "GoAI/common/code"

// Response 定义了API响应的通用结构
// StatusCode: 状态码，使用预定义的错误码
// StatusMsg: 状态消息，可选字段，用于描述状态详情
type Response struct {
	StatusCode code.Code `json:"status_code"`
	StatusMsg  string    `json:"status_msg,omitempty"`
}

// 设置响应的状态码和消息
func (r *Response) CodeOf(code code.Code) Response {
	if nil == r {
		r = new(Response)
	}
	r.StatusCode = code
	r.StatusMsg = code.Msg() // 获取对应状态码的默认消息
	return *r
}

// 用于快速设置成功状态
func (r *Response) Success() {
	r.CodeOf(code.CodeSuccess)
}
