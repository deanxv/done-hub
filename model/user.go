package model

import (
	"errors"
	"fmt"
	"go-template/common"
	"go-template/common/config"
	"go-template/common/logger"
	"go-template/common/utils"
	"strings"

	"gorm.io/gorm"
)

// User if you add sensitive fields, don't forget to clean them in setupLogin function.
// Otherwise, the sensitive information will be saved on local storage in plain text!
// User 用户表（登录/权限/账号状态等）
type User struct {
	// Id 主键自增ID
	Id int `json:"id" gorm:"comment:主键ID"`
	// Username 用户名（唯一，用于登录），最大12字符
	Username string `json:"username" gorm:"uniqueIndex;type:varchar(64);comment:用户名" validate:"required,max=12"`
	// Password 密码（加密存储，最小8、最大20字符）
	Password string `json:"password" gorm:"not null;type:varchar(255);comment:密码Hash" validate:"min=8,max=20"`
	// DisplayName 显示名称（可搜索）
	DisplayName string `json:"display_name" gorm:"index;type:varchar(64);comment:显示名称" validate:"max=20"`
	// Role 角色（0访客/1普通/10管理员/100超管）
	Role int `json:"role" gorm:"type:int;default:1;comment:角色(0访客/1普通/10管理员/100超管)"`
	// Status 状态（1启用/2禁用）
	Status int `json:"status" gorm:"type:int;default:1;comment:状态(1启用/2禁用)"`
	// Email 邮箱（可选，用于找回密码/通知）
	Email string `json:"email" gorm:"index;type:varchar(100);comment:邮箱" validate:"max=50"`
	// AvatarUrl 头像地址（可选）
	AvatarUrl string `json:"avatar_url" gorm:"type:varchar(500);column:avatar_url;default:'';comment:头像URL"`
	// OidcId OIDC Subject ID（开启 OIDC 登录时回填）
	OidcId string `json:"oidc_id" gorm:"column:oidc_id;index;type:varchar(255);comment:OIDC Subject ID"`
	// AccessToken 用户访问令牌（无 Session 时用于 API 鉴权，唯一）
	AccessToken string `json:"access_token" gorm:"type:char(32);column:access_token;uniqueIndex;comment:访问令牌"`
	// VerificationCode 非持久化字段：注册/绑定时的验证码
	VerificationCode string `json:"verification_code" gorm:"-:all"`
	// LastLoginTime 最近登录时间（Unix 秒）
	LastLoginTime int64 `json:"last_login_time" gorm:"bigint;default:0;comment:最近登录时间(Unix秒)"`
	// CreatedTime 账户创建时间（Unix 秒）
	CreatedTime int64 `json:"created_time" gorm:"bigint;comment:创建时间(Unix秒)"`
	// DeletedAt 软删除时间（用于软删除）
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index;comment:软删除时间"`
}

// 注意：最小版已移除用户配额与邀请等业务字段与缓存键

type UserUpdates func(*User)

func GetMaxUserId() int {
	var user User
	DB.Last(&user)
	return user.Id
}

var allowedUserOrderFields = map[string]bool{
	"id":           true,
	"username":     true,
	"role":         true,
	"status":       true,
	"created_time": true,
}

func GetUsersList(params *GenericParams) (*DataResult[User], error) {
	var users []*User
	db := DB.Omit("password")
	if params.Keyword != "" {
		db = db.Where("id = ? or username LIKE ? or email LIKE ? or display_name LIKE ?",
			utils.String2Int(params.Keyword),
			params.Keyword+"%", params.Keyword+"%", params.Keyword+"%",
		)
	}

	return PaginateAndOrder[User](db, &params.PaginationParams, &users, allowedUserOrderFields)
}

func GetUserById(id int, selectAll bool) (*User, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	user := User{Id: id}
	var err error = nil
	if selectAll {
		err = DB.First(&user, "id = ?", id).Error
	} else {
		err = DB.Omit("password").First(&user, "id = ?", id).Error
	}
	return &user, err
}

// 已移除 Telegram 相关能力

// 已移除推广码相关能力

func DeleteUserById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空！")
	}
	user := User{Id: id}
	return user.Delete()
}

func (user *User) Insert(inviterId int) error {
	if strings.TrimSpace(user.Username) == "" {
		return errors.New("用户名不能为空！")
	}
	if RecordExists(&User{}, "username", user.Username, nil) {
		return errors.New("用户名已存在！")
	}

	// 如果提供了邮箱，进行严格验证
	if user.Email != "" {
		if err := common.ValidateEmailStrict(user.Email); err != nil {
			return errors.New("邮箱格式不符合要求")
		}
	}
	var err error
	if user.Password != "" {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}
	user.AccessToken = utils.GetUUID()
	user.CreatedTime = utils.GetTimestamp()
	result := DB.Create(user)
	if result.Error != nil {
		return result.Error
	}
	// 业务赠送/返利日志已移除
	return nil
}

// InsertWithTx 在指定事务中创建用户
func (user *User) InsertWithTx(tx *gorm.DB, inviterId int) error {
	if strings.TrimSpace(user.Username) == "" {
		return errors.New("用户名不能为空！")
	}
	if RecordExistsWithTx(tx, &User{}, "username", user.Username, nil) {
		return errors.New("用户名已存在！")
	}

	// 如果提供了邮箱，进行严格验证
	if user.Email != "" {
		if err := common.ValidateEmailStrict(user.Email); err != nil {
			return errors.New("邮箱格式不符合要求")
		}
	}
	var err error
	if user.Password != "" {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}
	user.AccessToken = utils.GetUUID()
	user.CreatedTime = utils.GetTimestamp()
	result := tx.Create(user)
	if result.Error != nil {
		return result.Error
	}
	// 业务赠送/返利日志已移除
	return nil
}

func (user *User) Update(updatePassword bool) error {
	var err error
	omitFields := []string{}

	if updatePassword {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	} else {
		omitFields = append(omitFields, "password")
	}

	err = DB.Model(user).Omit(omitFields...).Updates(user).Error

	if err == nil && user.Role == config.RoleRootUser {
		config.RootUserEmail = user.Email
	}

	return err
}

func UpdateUser(id int, fields map[string]interface{}) error {
	err := DB.Model(&User{}).Where("id = ?", id).Updates(fields).Error
	if err != nil {
		return err
	}

	return nil
}

// ClearUserGroupAndTokensCache 清理用户分组和所有Token的缓存
// 清理 Token 与分组缓存逻辑已移除

func (user *User) Delete() error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}

	// 不改变当前数据库索引，通过更改用户名来删除用户
	user.Username = user.Username + "_del_" + utils.GetRandomString(6)
	err := user.Update(false)
	if err != nil {
		return err
	}

	err = DB.Delete(user).Error
	return err
}

// ValidateAndFill check password & user status
func (user *User) ValidateAndFill() (err error) {
	// When querying with struct, GORM will only query with non-zero fields,
	// that means if your field's value is 0, '', false or other zero values,
	// it won't be used to build query conditions
	password := user.Password
	if strings.TrimSpace(user.Username) == "" || strings.TrimSpace(password) == "" {
		return errors.New("用户名或密码为空")
	}
	err = DB.Where("username = ?", user.Username).First(user).Error
	if err != nil {
		// we must make sure check username firstly
		// consider this case: a malicious user set his username as other's email
		err := DB.Where("email = ?", user.Username).First(user).Error
		if err != nil {
			return errors.New("用户名或密码错误，或用户已被封禁")
		}
	}
	okay := common.ValidatePasswordAndHash(password, user.Password)
	if !okay || user.Status != config.UserStatusEnabled {
		return errors.New("用户名或密码错误，或用户已被封禁")
	}
	return nil
}

func (user *User) FillUserById() error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}

	result := DB.Where(User{Id: user.Id}).First(user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return errors.New("没有找到用户！")
		}
		return result.Error
	}
	return nil
}

func (user *User) FillUserByEmail() error {
	if user.Email == "" {
		return errors.New("email 为空！")
	}

	result := DB.Where(User{Email: user.Email}).First(user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return errors.New("没有找到用户！")
		}
		return result.Error
	}
	return nil
}

// Third-party ID based helpers are removed in the minimal edition.

func (user *User) FillUserByOidcId() error {
	if user.OidcId == "" {
		return errors.New("OIDC ID 为空！")
	}
	result := DB.Where(User{OidcId: user.OidcId}).First(user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return errors.New("没有找到用户！")
		}
		return result.Error
	}
	return nil
}

func (user *User) FillUserByUsername() error {
	if user.Username == "" {
		return errors.New("username 为空！")
	}
	result := DB.Where(User{Username: user.Username}).First(user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return errors.New("没有找到用户！")
		}
		return result.Error
	}
	return nil
}

func FindUserByField(field string, value any) (*User, error) {
	user := &User{}
	err := DB.Where(fmt.Sprintf("%s = ?", field), value).First(user).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	return user, err
}

func IsFieldAlreadyTaken(field string, value any) bool {
	var count int64
	DB.Model(&User{}).Where(fmt.Sprintf("%s = ?", field), value).Limit(1).Count(&count)
	return count > 0
}

func IsUsernameAlreadyTaken(username string) bool {
	return IsFieldAlreadyTaken("username", username)
}

func IsEmailAlreadyTaken(email string) bool {
	return IsFieldAlreadyTaken("email", email)
}

// Third-party id duplicate-check helpers are removed in the minimal edition.

func ResetUserPasswordByEmail(email string, password string) error {
	if email == "" || password == "" {
		return errors.New("邮箱地址或密码为空！")
	}
	hashedPassword, err := common.Password2Hash(password)
	if err != nil {
		return err
	}
	err = DB.Model(&User{}).Where("email = ?", email).Update("password", hashedPassword).Error
	return err
}

func IsAdmin(userId int) bool {
	if userId == 0 {
		return false
	}
	var user User
	err := DB.Where("id = ?", userId).Select("role").Find(&user).Error
	if err != nil {
		logger.SysError("no such user " + err.Error())
		return false
	}
	return user.Role >= config.RoleAdminUser
}

func IsUserEnabled(userId int) (bool, error) {
	if userId == 0 {
		return false, errors.New("user id is empty")
	}
	var user User
	err := DB.Where("id = ?", userId).Select("status").Find(&user).Error
	if err != nil {
		return false, err
	}
	return user.Status == config.UserStatusEnabled, nil
}

func ValidateAccessToken(token string) (user *User) {
	if token == "" {
		return nil
	}
	token = strings.Replace(token, "Bearer ", "", 1)
	user = &User{}
	if DB.Where("access_token = ?", token).First(user).RowsAffected == 1 {
		return user
	}
	return nil
}

// 已移除：GetUserFields / 配额 / 分组等与业务强绑定的方法

// 已移除：配额增减与批量更新逻辑

func GetRootUserEmail() (email string) {
	DB.Model(&User{}).Where("role = ?", config.RoleRootUser).Select("email").Find(&email)
	return email
}

// 已移除：使用量与请求计数统计

func GetUsernameById(id int) (username string) {
	DB.Model(&User{}).Where("id = ?", id).Select("username").Find(&username)
	return username
}

// 已移除：邀请统计与配额变更

// ProcessInviterReward is removed in the minimal edition (no inviter mechanics).
