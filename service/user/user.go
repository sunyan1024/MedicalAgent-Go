package user

import (
	"GoAI/common/code"
	myemail "GoAI/common/email"
	myredis "GoAI/common/redis"
	"GoAI/dao/user"
	"GoAI/model"
	"GoAI/utils"
	"GoAI/utils/myjwt"
)

// 用户登录 支持用户 UID或邮箱登录 使用bcrypt加密
func Login(identifier, password string) (string, code.Code) {
	var userInformation *model.User
	var ok bool

	//1:判断用户是否存在
	if ok, userInformation = user.IsExistUserByUIDOrEmail(identifier); !ok {
		return "", code.CodeUserNotExist
	}

	//2:判断用户是否密码账号正确
	if !user.VerifyPassword(userInformation, password) {
		return "", code.CodeInvalidPassword
	}

	//3:返回一个Token
	token, err := myjwt.GenerateToken(userInformation.UID)
	if err != nil {
		return "", code.CodeServerBusy
	}

	return token, code.CodeSuccess
}

// 用户注册功能
func Register(email, password, captcha, nickname string) (string, code.Code) {
	var ok bool
	var userInformation *model.User

	//1:先判断用户是否已经存在了
	if ok, _ := user.IsExistUserByUIDOrEmail(email); ok {
		return "", code.CodeUserExist
	}

	//2:从redis中验证验证码是否有效
	if ok, _ := myredis.CheckCaptchaForEmail(email, captcha); !ok {
		return "", code.CodeInvalidCaptcha
	}

	//3：生成11位的账号
	uid := utils.GetRandomNumbers(11)

	//4：注册到数据库中
	if userInformation, ok = user.Register(uid, email, password, nickname); !ok {
		return "", code.CodeServerBusy
	}

	//5：将账号一并发送到对应邮箱上去，后续需要账号登录
	if err := myemail.SendCaptcha(email, uid, myemail.UIDMsg); err != nil {
		return "", code.CodeServerBusy
	}

	// 6:生成Token
	token, err := myjwt.GenerateToken(userInformation.UID)
	if err != nil {
		return "", code.CodeServerBusy
	}

	return token, code.CodeSuccess
}

// 往指定邮箱发送验证码
//
// 分为以下任务：
// 1：先存放redis
// 2：再进行远程发送
func SendCaptcha(email string) code.Code {
	captcha := utils.GetRandomNumbers(6) // 生成6位随机数，作为验证码

	//1:先存放到redis
	if err := myredis.SetCaptchaForEmail(email, captcha); err != nil {
		return code.CodeServerBusy
	}

	//2:再进行远程发送
	if err := myemail.SendCaptcha(email, captcha, myemail.CodeMsg); err != nil {
		return code.CodeServerBusy
	}

	return code.CodeSuccess
}
