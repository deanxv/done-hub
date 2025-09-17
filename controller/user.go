package controller

import (
	"encoding/json"
	"go-template/common"
	"go-template/common/config"
	"go-template/common/utils"
	"go-template/model"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// getFriendlyValidationMessage 将验证错误转换为友好的中文提示
func getFriendlyValidationMessage(err error) string {
	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		for _, fieldError := range validationErrors {
			field := fieldError.Field()
			tag := fieldError.Tag()

			switch field {
			case "Username":
				switch tag {
				case "required":
					return "用户名不能为空"
				case "max":
					return "用户名长度不能超过12个字符"
				}
			case "Password":
				switch tag {
				case "required":
					return "密码不能为空"
				case "min":
					return "密码长度不能少于8个字符"
				case "max":
					return "密码长度不能超过20个字符"
				}
			case "DisplayName":
				switch tag {
				case "max":
					return "显示名称长度不能超过20个字符"
				}
			case "Email":
				switch tag {
				case "email":
					return "邮箱格式不正确"
				case "max":
					return "邮箱长度不能超过50个字符"
				}
			}
		}
	}
	return "输入参数不符合要求"
}

// Login godoc
// @Summary User login
// @Description 用户名密码登录
// @Tags User
// @Accept json
// @Produce json
// @Param body body LoginRequest true "登录请求"
// @Success 200 {object} map[string]interface{}
// @Router /user/login [post]
func Login(c *gin.Context) {
	if !config.PasswordLoginEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "管理员关闭了密码登录",
			"success": false,
		})
		return
	}
	var loginRequest LoginRequest
	err := json.NewDecoder(c.Request.Body).Decode(&loginRequest)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": "无效的参数",
			"success": false,
		})
		return
	}
	username := loginRequest.Username
	password := loginRequest.Password
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		c.JSON(http.StatusOK, gin.H{
			"message": "无效的参数",
			"success": false,
		})
		return
	}
	user := model.User{
		Username: username,
		Password: password,
	}
	err = user.ValidateAndFill()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	setupLogin(&user, c)
}

// setup session & cookies and then return user info
func setupLogin(user *model.User, c *gin.Context) {
	session := sessions.Default(c)
	session.Set("id", user.Id)
	session.Set("username", user.Username)
	session.Set("role", user.Role)
	session.Set("status", user.Status)
	err := session.Save()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": "无法保存会话信息，请重试",
			"success": false,
		})
		return
	}
	user.LastLoginTime = time.Now().Unix()

	user.Update(false)

	cleanUser := model.User{
		Id:          user.Id,
		AvatarUrl:   user.AvatarUrl,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		Status:      user.Status,
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
		"data":    cleanUser,
	})
}

// Logout godoc
// @Summary User logout
// @Description 注销当前会话
// @Tags User
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /user/logout [get]
func Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	err := session.Save()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
	})
}

// Register 保留最小化的用户名+密码注册能力；移除邀请码/返利等业务逻辑。
// Register godoc
// @Summary User register
// @Description 用户注册（用户名+密码，可选邮箱验证）
// @Tags User
// @Accept json
// @Produce json
// @Param body body model.User true "注册信息"
// @Success 200 {object} map[string]interface{}
// @Router /user/register [post]
func Register(c *gin.Context) {
	if !config.RegisterEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员关闭了新用户注册",
		})
		return
	}
	if !config.PasswordRegisterEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员关闭了通过密码进行注册",
		})
		return
	}

	var user model.User
	if err := c.ShouldBindJSON(&user); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}

	if strings.TrimSpace(user.Password) == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "密码不能为空",
		})
		return
	}

	if err := common.Validate.Struct(&user); err != nil {
		friendlyMessage := getFriendlyValidationMessage(err)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": friendlyMessage,
		})
		return
	}

	// 可选：邮箱验证（若启用）
	if config.EmailVerificationEnabled {
		if user.Email == "" || user.VerificationCode == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "管理员开启了邮箱验证，请输入邮箱地址和验证码",
			})
			return
		}
		if err := common.ValidateEmailStrict(user.Email); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "邮箱格式不符合要求",
			})
			return
		}
		if !common.VerifyCodeWithKey(user.Email, user.VerificationCode, common.EmailVerificationPurpose) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "验证码错误或已过期",
			})
			return
		}
	}

	cleanUser := model.User{
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.Username,
	}
	if config.EmailVerificationEnabled {
		cleanUser.Email = user.Email
	}

	if err := cleanUser.Insert(0); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// GetUsersList godoc
// @Summary List users (admin)
// @Description 获取用户列表（管理员）
// @Tags Admin
// @Produce json
// @Param page query int false "页码"
// @Param size query int false "每页数量"
// @Param order query string false "排序，如 -id,username"
// @Param keyword query string false "搜索关键字"
// @Success 200 {object} map[string]interface{}
// @Router /user/ [get]
func GetUsersList(c *gin.Context) {
	var params model.GenericParams
	if err := c.ShouldBindQuery(&params); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	users, err := model.GetUsersList(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    users,
	})
}

// GetUser godoc
// @Summary Get user (admin)
// @Description 获取用户详情（管理员）
// @Tags Admin
// @Produce json
// @Param id path int true "用户ID"
// @Success 200 {object} map[string]interface{}
// @Router /user/{id} [get]
func GetUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	user, err := model.GetUserById(id, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	myRole := c.GetInt("role")
	if myRole <= user.Role && myRole != config.RoleRootUser {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权获取同级或更高等级用户的信息",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user,
	})
}

const API_LIMIT_KEY = "api-limiter:%d"

// 已移除与速率/分组/配额相关的统计接口

// 已移除与统计看板相关接口

// 生成用户 AccessToken（用于无 Session 的 API 调用）
// GenerateAccessToken godoc
// @Summary Generate access token
// @Description 生成用户 AccessToken（无 Session 的 API 鉴权使用）
// @Tags User
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /user/token [get]
func GenerateAccessToken(c *gin.Context) {
	id := c.GetInt("id")
	user, err := model.GetUserById(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	user.AccessToken = utils.GetUUID()

	if model.DB.Where("access_token = ?", user.AccessToken).First(user).RowsAffected != 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "请重试，系统生成的 UUID 竟然重复了！",
		})
		return
	}

	if err := user.Update(false); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user.AccessToken,
	})
}

// 已移除推广码接口

// GetSelf godoc
// @Summary Get self profile
// @Description 获取当前用户信息
// @Tags User
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /user/self [get]
func GetSelf(c *gin.Context) {
	id := c.GetInt("id")
	user, err := model.GetUserById(id, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// 邀请相关统计在最小版中移除

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user,
	})
}

// UpdateUser godoc
// @Summary Update user (admin)
// @Description 更新用户信息（管理员）
// @Tags Admin
// @Accept json
// @Produce json
// @Param body body model.User true "用户信息"
// @Success 200 {object} map[string]interface{}
// @Router /user/ [put]
func UpdateUser(c *gin.Context) {
	var updatedUser model.User
	err := json.NewDecoder(c.Request.Body).Decode(&updatedUser)
	if err != nil || updatedUser.Id == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	if updatedUser.Password == "" {
		updatedUser.Password = "$I_LOVE_U" // make Validator happy :)
	}
	if err := common.Validate.Struct(&updatedUser); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "输入不合法 " + err.Error(),
		})
		return
	}

	// 如果更新了邮箱，进行严格验证
	if updatedUser.Email != "" {
		if err := common.ValidateEmailStrict(updatedUser.Email); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "邮箱格式不符合要求",
			})
			return
		}
	}
	originUser, err := model.GetUserById(updatedUser.Id, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	myRole := c.GetInt("role")
	if myRole <= originUser.Role && myRole != config.RoleRootUser {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权更新同权限等级或更高权限等级的用户信息",
		})
		return
	}
	if myRole <= updatedUser.Role && myRole != config.RoleRootUser {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权将其他用户权限等级提升到大于等于自己的权限等级",
		})
		return
	}
	if updatedUser.Password == "$I_LOVE_U" {
		updatedUser.Password = "" // rollback to what it should be
	}
	updatePassword := updatedUser.Password != ""
	if err := updatedUser.Update(updatePassword); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// UpdateSelf godoc
// @Summary Update self profile
// @Description 更新当前用户信息
// @Tags User
// @Accept json
// @Produce json
// @Param body body model.User true "用户信息"
// @Success 200 {object} map[string]interface{}
// @Router /user/self [put]
func UpdateSelf(c *gin.Context) {
	var user model.User
	err := json.NewDecoder(c.Request.Body).Decode(&user)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	if user.Password == "" {
		user.Password = "$I_LOVE_U" // make Validator happy :)
	}
	if err := common.Validate.Struct(&user); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "输入不合法 " + err.Error(),
		})
		return
	}

	cleanUser := model.User{
		Id: c.GetInt("id"),
		// Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.DisplayName,
	}
	if user.Password == "$I_LOVE_U" {
		user.Password = "" // rollback to what it should be
		cleanUser.Password = ""
	}
	updatePassword := user.Password != ""
	if err := cleanUser.Update(updatePassword); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// DeleteUser godoc
// @Summary Delete user (admin)
// @Description 删除用户（管理员）
// @Tags Admin
// @Produce json
// @Param id path int true "用户ID"
// @Success 200 {object} map[string]interface{}
// @Router /user/{id} [delete]
func DeleteUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	originUser, err := model.GetUserById(id, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	myRole := c.GetInt("role")
	if myRole <= originUser.Role {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权删除同权限等级或更高权限等级的用户",
		})
		return
	}
	err = model.DeleteUserById(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
		})
		return
	}
}

// CreateUser godoc
// @Summary Create user (admin)
// @Description 创建用户（管理员）
// @Tags Admin
// @Accept json
// @Produce json
// @Param body body model.User true "用户信息"
// @Success 200 {object} map[string]interface{}
// @Router /user/ [post]
func CreateUser(c *gin.Context) {
	var user model.User
	err := c.ShouldBindJSON(&user)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// 管理员创建用户时的特定验证
	if strings.TrimSpace(user.Username) == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户名不能为空",
		})
		return
	}

	if strings.TrimSpace(user.Password) == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "密码不能为空",
		})
		return
	}
	if err := common.Validate.Struct(&user); err != nil {
		// 友好的验证错误提示
		friendlyMessage := getFriendlyValidationMessage(err)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": friendlyMessage,
		})
		return
	}

	// 如果提供了邮箱，进行严格验证
	if user.Email != "" {
		if err := common.ValidateEmailStrict(user.Email); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "邮箱格式不符合要求",
			})
			return
		}
	}

	if user.DisplayName == "" {
		user.DisplayName = user.Username
	}
	myRole := c.GetInt("role")
	if user.Role >= myRole {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无法创建权限大于等于自己的用户",
		})
		return
	}
	// Even for admin users, we cannot fully trust them!
	cleanUser := model.User{
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.DisplayName,
	}
	if err := cleanUser.Insert(0); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

type ManageRequest struct {
	UserId int    `json:"user_id"`
	Action string `json:"action"`
}

// ManageUser Only admin user can do this
// ManageUser godoc
// @Summary Manage user (admin)
// @Description 管理用户（封禁、升级/降级等）
// @Tags Admin
// @Accept json
// @Produce json
// @Param body body ManageRequest true "管理操作"
// @Success 200 {object} map[string]interface{}
// @Router /user/manage [post]
func ManageUser(c *gin.Context) {
	var req ManageRequest
	err := json.NewDecoder(c.Request.Body).Decode(&req)

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}

	if req.UserId == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户ID不能为空",
		})
		return
	}

	user, err := model.GetUserById(req.UserId, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户不存在",
		})
		return
	}
	myRole := c.GetInt("role")
	if myRole <= user.Role && myRole != config.RoleRootUser {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权更新同权限等级或更高权限等级的用户信息",
		})
		return
	}
	switch req.Action {
	case "disable":
		user.Status = config.UserStatusDisabled
		if user.Role == config.RoleRootUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法禁用超级管理员用户",
			})
			return
		}
	case "enable":
		user.Status = config.UserStatusEnabled
	case "delete":
		if user.Role == config.RoleRootUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法删除超级管理员用户",
			})
			return
		}
		if err := user.Delete(); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	case "promote":
		if myRole != config.RoleRootUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "普通管理员用户无法提升其他用户为管理员",
			})
			return
		}
		if user.Role >= config.RoleAdminUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "该用户已经是管理员",
			})
			return
		}
		user.Role = config.RoleAdminUser
	case "demote":
		if user.Role == config.RoleRootUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法降级超级管理员用户",
			})
			return
		}
		if user.Role == config.RoleCommonUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "该用户已经是普通用户",
			})
			return
		}
		user.Role = config.RoleCommonUser
	}

	if err := user.Update(false); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	clearUser := model.User{
		Role:   user.Role,
		Status: user.Status,
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    clearUser,
	})
}

// EmailBind godoc
// @Summary Bind email with code
// @Description 绑定邮箱
// @Tags User
// @Produce json
// @Param email query string true "邮箱"
// @Param code query string true "验证码"
// @Success 200 {object} map[string]interface{}
// @Router /oauth/email/bind [get]
func EmailBind(c *gin.Context) {
	email := c.Query("email")
	code := c.Query("code")

	// 严格验证邮箱格式
	if err := common.ValidateEmailStrict(email); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "邮箱格式不符合要求",
		})
		return
	}

	if !common.VerifyCodeWithKey(email, code, common.EmailVerificationPurpose) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "验证码错误或已过期",
		})
		return
	}
	id := c.GetInt("id")
	user := model.User{
		Id: id,
	}
	err := user.FillUserById()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	user.Email = email
	// no need to check if this email already taken, because we have used verification code to check it
	err = user.Update(false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

type topUpRequest struct {
	Key string `json:"key"`
}

// 已移除充值相关接口

// 已移除修改配额接口
