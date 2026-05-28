package user

import (
	"GoAI/common/mysql"
	"GoAI/model"
	"GoAI/utils"
)

// 全局的空上下文
// var ctx = context.Background()

// 用户插入操作
func InsertUser(user *model.User) (*model.User, error) {
	err := mysql.DB.Create(&user).Error // 创建新用户记录
	return user, err
}

// 根据username查询用户
func GetUserByUsername(username string) (*model.User, error) {
	user := new(model.User)
	err := mysql.DB.Where("username = ?", username).First(user).Error // 根据用户名查询用户
	return user, err
}

// 根据email查询用户
func GetUserByEmail(email string) (*model.User, error) {
	user := new(model.User)
	err := mysql.DB.Where("email = ?", email).First(user).Error // 根据email查询用户
	return user, err
}

// 检查用户是否存在
func IsExistUser(identifier string) (bool, *model.User) {
	// 先尝试通过email查找
	user, err := GetUserByEmail(identifier)
	if err == nil {
		return true, user
	}

	// 如果email不存在，再尝试通过username查找
	user, err = GetUserByUsername(identifier)
	if err == nil {
		return true, user
	}

	return false, nil
}

// 添加一个更明确的函数名，以支持用户名或邮箱登录
func IsExistUserByUsernameOrEmail(identifier string) (bool, *model.User) {
	return IsExistUser(identifier)
}

// 处理用户注册 使用MD5加密密码
func RegisterMD5(username, email, password string) (*model.User, bool) {
	if user, err := InsertUser(&model.User{
		Email:    email,
		Name:     username,
		Username: username,
		Password: utils.MD5(password), // 使用MD5加密密码
	}); err != nil {
		return nil, false
	} else {
		return user, true
	}
}

// 处理用户注册 使用bcrypt加密密码
func Register(username, email, password string) (*model.User, bool) {
	hashedPassword, err := utils.GenerateFromPassword(password) // 使用bcrypt加密密码
	if err != nil {
		return nil, false
	}

	if user, err := InsertUser(&model.User{
		Email:    email,
		Name:     username,
		Username: username,
		Password: string(hashedPassword), // 存储bcrypt哈希值
	}); err != nil {
		return nil, false
	} else {
		return user, true
	}
}

// 验证密码函数
func VerifyPassword(user *model.User, password string) bool {
	// 使用bcrypt函数比较哈希值和明文密码
	err := utils.CompareHashAndPassword(user.Password, password)
	return err == nil
}
