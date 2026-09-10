package user

import (
	"GoAI/common/code"
	"GoAI/controller"
	"GoAI/model"
	"GoAI/service/user"
	"net/http"

	"github.com/gin-gonic/gin"
)

type (
	//这里的Username只能是账号登录，和我做的另一个项目有区别（邮箱账号均可)
	LoginRequest struct {
		Identifier string `json:"identifier"` // 可以是用户名或邮箱
		Password   string `json:"password"`
	}
	// omitempty当字段为空的时候，不返回这个东西
	LoginResponse struct {
		controller.Response
		Token string `json:"token,omitempty"`
	}
	//验证码由后端生成，存放到redis中，固然需要先发送一次请求CaptchaRequest,然后用返回的验证码
	//邮箱以及密码进行注册，后续再将账号进行返回
	RegisterRequest struct {
		Email    string `json:"email" binding:"required"`
		Captcha  string `json:"captcha"`
		Password string `json:"password"`
	}
	//注册成功之后，直接让其进行登录状态
	RegisterResponse struct {
		controller.Response
		Token string `json:"token,omitempty"`
	}

	CaptchaRequest struct {
		Email string `json:"email" binding:"required"`
	}

	CaptchaResponse struct {
		controller.Response
	}
)

// Login 处理用户登录请求
// 接收包含用户标识符和密码的JSON请求
// 成功时返回包含JWT令牌的响应
// 失败时返回对应的错误码和消息
func Login(c *gin.Context) {
	req := new(LoginRequest)
	res := new(LoginResponse)

	//解析参数
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	//给service层进行处理
	token, code_ := user.Login(req.Identifier, req.Password)
	if code_ != code.CodeSuccess {
		c.JSON(http.StatusOK, res.CodeOf(code_))
		return
	}

	res.Success()
	res.Token = token
	c.JSON(http.StatusOK, res)
}

// Register 处理用户注册请求
// 接收JSON格式的注册信息(email, password, captcha)
// 成功时返回JWT token，失败时返回错误码
// 参数验证失败返回CodeInvalidParams错误
func Register(c *gin.Context) {
	req := new(RegisterRequest)
	res := new(RegisterResponse)

	//解析参数
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	//给service层进行处理
	token, code_ := user.Register(req.Email, req.Password, req.Captcha)
	if code_ != code.CodeSuccess {
		c.JSON(http.StatusOK, res.CodeOf(code_))
		return
	}

	res.Success()
	res.Token = token

	c.JSON(http.StatusOK, res)
}

// HandleCaptcha 处理验证码发送请求
// 接收包含邮箱地址的JSON请求，向指定邮箱发送验证码
// 成功时返回成功状态，失败时返回对应的错误码
func HandleCaptcha(c *gin.Context) {
	req := new(CaptchaRequest)
	res := new(CaptchaResponse)

	//解析参数
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	//给service层进行处理
	code_ := user.SendCaptcha(req.Email)
	if code_ != code.CodeSuccess {
		c.JSON(http.StatusOK, res.CodeOf(code_))
		return
	}

	//匿名字段，其实本身res.Success()调用就是res.Response.Success()
	//res.Response.Success()
	res.Success()
	c.JSON(http.StatusOK, res)
}

func Me(c *gin.Context) {
	value, _ := c.Get("currentUser")
	account, ok := value.(*model.User)
	if !ok || account == nil {
		c.JSON(http.StatusUnauthorized, new(controller.Response).CodeOf(code.CodeInvalidToken))
		return
	}
	res := struct {
		controller.Response
		UserInfo *model.User `json:"user_info"`
	}{UserInfo: account}
	res.Success()
	c.JSON(http.StatusOK, res)
}
