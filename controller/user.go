package controller

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/limit"
	"done-hub/common/utils"
	"done-hub/model"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	"time"

	"gorm.io/gorm"

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
	user.LastLoginIp = c.ClientIP()

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

func Register(c *gin.Context) {
	if !config.RegisterEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "管理员关闭了新用户注册",
			"success": false,
		})
		return
	}
	if !config.PasswordRegisterEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "管理员关闭了通过密码进行注册，请使用第三方账户验证的形式进行注册",
			"success": false,
		})
		return
	}
	var user model.User
	err := c.ShouldBindJSON(&user)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	// 密码注册特定验证
	if strings.TrimSpace(user.Password) == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "密码不能为空",
		})
		return
	}

	// 管理员开启用户协议/隐私政策时，注册需同意相关协议
	if (config.UserAgreementEnabled || config.PrivacyPolicyEnabled) && !user.Agreed {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "请先阅读并同意相关协议",
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
	if config.EmailVerificationEnabled {
		if user.Email == "" || user.VerificationCode == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "管理员开启了邮箱验证，请输入邮箱地址和验证码",
			})
			return
		}

		// 严格验证邮箱格式
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

	// 邀请码基本验证（仅适用于密码注册，三方登录注册不需要邀请码）
	if config.InviteCodeRegisterEnabled {
		if user.InviteCode == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "管理员开启了邀请码注册，请输入邀请码",
			})
			return
		}
	}

	affCode := user.AffCode // this code is the inviter's code, not the user's own code
	inviterId, _ := model.GetUserIdByAffCode(affCode)
	cleanUser := model.User{
		Username:    user.Username,
		Password:    user.Password,
		DisplayName: user.Username,
		InviterId:   inviterId,
	}

	// 只有启用邀请码注册时才保存使用的邀请码
	if config.InviteCodeRegisterEnabled && user.InviteCode != "" {
		cleanUser.UsedInviteCode = user.InviteCode
	}

	// 注册通过协议校验后，对已开启的协议分别记录当前同意版本
	if config.UserAgreementEnabled {
		cleanUser.AgreedUserAgreementVersion = agreementVersion(config.GlobalOption.Get("UserAgreement"))
	}
	if config.PrivacyPolicyEnabled {
		cleanUser.AgreedPrivacyPolicyVersion = agreementVersion(config.GlobalOption.Get("PrivacyPolicy"))
	}

	if config.EmailVerificationEnabled {
		cleanUser.Email = user.Email
	}

	// 如果需要使用邀请码，先获取锁（按照order.go的模式）
	if config.InviteCodeRegisterEnabled && user.InviteCode != "" {
		// 优先使用Redis分布式锁，失败时使用内存锁
		if config.RedisEnabled {
			mutex, lockErr := model.AcquireInviteCodeLock(user.InviteCode)
			if lockErr == nil && mutex != nil {
				defer func() {
					unlockOk, unlockErr := mutex.Unlock()
					if unlockErr != nil || !unlockOk {
						// 注册过程中的解锁失败不应该影响用户注册结果，只记录日志
						// logger.SysError(fmt.Sprintf("failed to unlock invite code %s: ok=%v, err=%v", user.InviteCode, unlockOk, unlockErr))
					}
				}()
			} else {
				// Redis锁失败，降级到内存锁
				model.LockInviteCode(user.InviteCode)
				defer model.UnlockInviteCode(user.InviteCode)
			}
		} else {
			// 无Redis时使用内存锁
			model.LockInviteCode(user.InviteCode)
			defer model.UnlockInviteCode(user.InviteCode)
		}

		// 在锁保护下验证邀请码（防止TOCTOU攻击）
		err := model.CheckInviteCode(user.InviteCode)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}

	// 使用事务确保用户创建和邀请码使用的原子性
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		// 在事务中创建用户
		if err := cleanUser.InsertWithTx(tx, inviterId); err != nil {
			return err
		}

		// 在事务中增加邀请码使用次数
		if config.InviteCodeRegisterEnabled && user.InviteCode != "" {
			if err := model.UseInviteCodeWithTx(tx, user.InviteCode); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// 事务提交成功后，刷新相关缓存
	if config.RedisEnabled {
		// 刷新用户配额缓存（如果有邀请奖励）
		if inviterId != 0 && config.QuotaForInviter > 0 {
			model.CacheUpdateUserQuota(inviterId)
		}
		if config.QuotaForInvitee > 0 {
			model.CacheUpdateUserQuota(cleanUser.Id)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

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

func GetRateRealtime(c *gin.Context) {
	id := c.GetInt("id")
	user, err := model.GetUserById(id, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	limiter := model.GlobalUserGroupRatio.GetAPILimiter(user.Group)
	key := fmt.Sprintf(API_LIMIT_KEY, id)
	// 获取当前已使用的速率
	rpm, err := limiter.GetCurrentRate(key)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	maxRPM := limit.GetMaxRate(limiter)
	var usageRpmRate float64 = 0
	if maxRPM > 0 {
		usageRpmRate = math.Floor(float64(rpm)/float64(maxRPM)*100*100) / 100
	}

	data := map[string]interface{}{
		"rpm":          rpm,
		"maxRPM":       maxRPM,
		"usageRpmRate": usageRpmRate,
		"tpm":          0,
		"maxTPM":       0,
		"usageTpmRate": 0,
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
}

func GetUserDashboard(c *gin.Context) {
	id := c.GetInt("id")

	// 使用 TZ 环境变量的时区，与 UpdateStatistics 保持一致
	location := time.Local
	if tzEnv := os.Getenv("TZ"); tzEnv != "" {
		if loc, err := time.LoadLocation(tzEnv); err == nil {
			location = loc
		}
	}
	now := time.Now().In(location)
	toDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	endOfDay := toDay.Add(-time.Second).Add(time.Hour * 24).Format("2006-01-02")
	startOfDay := toDay.AddDate(0, 0, -7).Format("2006-01-02")

	dashboards, err := model.GetUserModelStatisticsByPeriod(id, startOfDay, endOfDay)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无法获取统计信息.",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    dashboards,
	})
}

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

func GetAffCode(c *gin.Context) {
	id := c.GetInt("id")
	user, err := model.GetUserById(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if user.AffCode == "" {
		user.AffCode = utils.GetRandomString(4)
		if err := user.Update(false); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user.AffCode,
	})
}

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

	// 实时计算邀请人数
	affCount, err := model.GetUserInviteCount(id)
	if err == nil {
		user.AffCount = int(affCount)
	}

	// 实时计算是否需要（重新）同意协议：与注册/AgreeToTerms 同一算法同一正文源。
	// 判定收归服务端，前端只消费布尔，避免依赖 siteInfo 刷新时机。
	uaVer := agreementVersion(config.GlobalOption.Get("UserAgreement"))
	user.NeedAgreeUserAgreement = config.UserAgreementEnabled && uaVer != "" && user.AgreedUserAgreementVersion != uaVer
	ppVer := agreementVersion(config.GlobalOption.Get("PrivacyPolicy"))
	user.NeedAgreePrivacyPolicy = config.PrivacyPolicyEnabled && ppVer != "" && user.AgreedPrivacyPolicyVersion != ppVer

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    user,
	})
}

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
	if originUser.Quota != updatedUser.Quota {
		model.RecordLog(originUser.Id, model.LogTypeManage, fmt.Sprintf("管理员将用户额度从 %s修改为 %s", common.LogQuota(originUser.Quota), common.LogQuota(updatedUser.Quota)))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

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
		// 设置为管理员：只有超级管理员能操作
		if myRole != config.RoleRootUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "只有超级管理员可以设置其他用户为管理员",
			})
			return
		}
		if user.Role == config.RoleRootUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法修改超级管理员的身份",
			})
			return
		}
		user.Role = config.RoleAdminUser
	case "demote":
		// 设置为普通用户：不能操作超级管理员
		if user.Role == config.RoleRootUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法修改超级管理员的身份",
			})
			return
		}
		user.Role = config.RoleCommonUser
	case "set_reliable":
		// 设置为可信内部员工：管理员及以上能操作
		if myRole < config.RoleAdminUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "只有管理员或超级管理员可以设置可信内部员工",
			})
			return
		}
		if user.Role == config.RoleRootUser {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无法修改超级管理员的身份",
			})
			return
		}
		user.Role = config.RoleReliableUser
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

func TopUp(c *gin.Context) {
	req := topUpRequest{}
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	id := c.GetInt("id")
	quota, err := model.Redeem(req.Key, id, c.ClientIP())
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
		"data":    quota,
	})
}

type ChangeUserQuotaRequest struct {
	Quota  int    `json:"quota" form:"quota"`
	Remark string `json:"remark" form:"remark"`
}

func ChangeUserQuota(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	var req ChangeUserQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if req.Quota == 0 {
		common.APIRespondWithError(c, http.StatusOK, errors.New("不能为0"))
		return
	}

	err = model.ChangeUserQuota(userId, req.Quota, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	remark := fmt.Sprintf("管理员增减用户额度 %s", common.LogQuota(req.Quota))

	if req.Remark != "" {
		remark = fmt.Sprintf("%s, 备注: %s", remark, req.Remark)
	}

	model.RecordQuotaLog(userId, model.LogTypeManage, req.Quota, c.ClientIP(), remark)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

type UnbindRequest struct {
	Type string `json:"type"`
}

func Unbind(c *gin.Context) {
	var req UnbindRequest
	err := json.NewDecoder(c.Request.Body).Decode(&req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的参数",
		})
		return
	}
	id := c.GetInt("id")
	user, err := model.GetUserById(id, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	updates := make(map[string]interface{})
	switch req.Type {
	case "github":
		updates["github_id"] = ""
		updates["github_id_new"] = nil
	case "wechat":
		updates["wechat_id"] = ""
	case "lark":
		updates["lark_id"] = ""
	case "oidc":
		updates["oidc_id"] = ""
	default:
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "未知的绑定类型",
		})
		return
	}
	err = model.DB.Model(user).Updates(updates).Error
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

// AgreeToTerms 当前登录用户重新同意协议：把服务端当前版本写入用户已同意版本字段。
// 版本一律服务端从正文即时计算，不信任前端传值，防伪造。
func AgreeToTerms(c *gin.Context) {
	id := c.GetInt("id")
	updates := map[string]interface{}{}
	if config.UserAgreementEnabled {
		updates["agreed_user_agreement_version"] = agreementVersion(config.GlobalOption.Get("UserAgreement"))
	}
	if config.PrivacyPolicyEnabled {
		updates["agreed_privacy_policy_version"] = agreementVersion(config.GlobalOption.Get("PrivacyPolicy"))
	}
	if len(updates) > 0 {
		if err := model.UpdateUser(id, updates); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}
