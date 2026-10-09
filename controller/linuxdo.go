package controller

import (
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/model"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LinuxDoUser struct {
	Id         int    `json:"id"`
	Username   string `json:"username"`
	Name       string `json:"name"`
	Active     bool   `json:"active"`
	TrustLevel int    `json:"trust_level"`
	Silenced   bool   `json:"silenced"`
}

func LinuxDoBind(c *gin.Context) {
	if !config.LinuxDoOAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未开启通过 LINUX DO 登录以及注册",
		})
		return
	}

	// CSRF 防护：验证 state 参数（防止攻击者通过构造的 code 参数覆盖绑定）
	session := sessions.Default(c)
	state := c.Query("state")
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "无效的 state 参数，请重新开始绑定流程",
		})
		return
	}

	// 验证通过后立即清理 state，防止重放攻击
	session.Delete("oauth_state")
	if err := session.Save(); err != nil {
		logger.SysError(fmt.Sprintf("Failed to delete oauth_state in LinuxDoBind: %v", err))
	}

	code := c.Query("code")
	linuxDoUser, err := getLinuxDoUserInfoByCode(code, c)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	if config.LinuxDoOAuthTrustLevelEnabled {
		if linuxDoUser.TrustLevel < config.LinuxDoOAuthLowestTrustLevel {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "该 LINUX DO 信任等级过低，不允许访问",
			})
			return
		}
	}

	user := model.User{
		LinuxDoId: linuxDoUser.Id,
	}

	if model.IsLinuxDOIdAlreadyTaken(user.LinuxDoId) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "该 LINUX DO 账户已被绑定",
		})
		return
	}

	// session 已在函数开头获取，这里直接使用
	idVal := session.Get("id")
	if idVal == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "会话无效，请重新登录",
		})
		return
	}
	userId, ok := idVal.(int)
	if !ok {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户 ID 类型错误",
		})
		return
	}
	user.Id = userId

	err = user.FillUserById()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	// 安全检查：防止覆盖式重绑攻击
	// 如果当前用户已有 LinuxDo 绑定，禁止直接覆盖
	if user.LinuxDoId != 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "当前账号已绑定 LINUX DO，如需更换请先解绑",
		})
		return
	}

	user.LinuxDoId = linuxDoUser.Id
	user.LinuxDoUsername = linuxDoUser.Username
	user.LinuxDoTrustLevel = linuxDoUser.TrustLevel
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
		"message": "bind",
	})
}

func getLinuxDoUserInfoByCode(code string, c *gin.Context) (*LinuxDoUser, error) {
	if code == "" {
		return nil, errors.New("invalid code")
	}

	// Get access token using Basic auth
	tokenEndpoint := "https://connect.linux.do/oauth2/token"
	credentials := config.LinuxDoClientId + ":" + config.LinuxDoClientSecret
	basicAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(credentials))

	// Get redirect URI from request
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	redirectURI := fmt.Sprintf("%s://%s/api/oauth/linuxdo", scheme, c.Request.Host)

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)

	req, err := http.NewRequest("POST", tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", basicAuth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("failed to connect to Linux DO server")
	}
	defer res.Body.Close()

	var tokenRes struct {
		AccessToken string `json:"access_token"`
		Message     string `json:"message"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tokenRes); err != nil {
		return nil, err
	}

	if tokenRes.AccessToken == "" {
		return nil, fmt.Errorf("failed to get access token: %s", tokenRes.Message)
	}

	// Get user info
	userEndpoint := "https://connect.linux.do/api/user"
	req, err = http.NewRequest("GET", userEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tokenRes.AccessToken)
	req.Header.Set("Accept", "application/json")

	res2, err := client.Do(req)
	if err != nil {
		return nil, errors.New("failed to get user info from Linux DO")
	}
	defer res2.Body.Close()

	var linuxDoUser LinuxDoUser
	if err := json.NewDecoder(res2.Body).Decode(&linuxDoUser); err != nil {
		return nil, err
	}

	if linuxDoUser.Id == 0 {
		return nil, errors.New("invalid user info returned")
	}

	return &linuxDoUser, nil
}

func LinuxDoOAuth(c *gin.Context) {
	session := sessions.Default(c)

	errorCode := c.Query("error")
	if errorCode != "" {
		errorDescription := c.Query("error_description")
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": errorDescription,
		})
		return
	}

	state := c.Query("state")
	if state == "" || session.Get("oauth_state") == nil || state != session.Get("oauth_state").(string) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "state is empty or not same",
		})
		return
	}

	username := session.Get("username")
	if username != nil {
		LinuxDoBind(c)
		return
	}

	// 验证通过后立即清理 state，防止重放攻击
	session.Delete("oauth_state")
	if err := session.Save(); err != nil {
		logger.SysError(fmt.Sprintf("Failed to delete oauth_state in LinuxDoOAuth: %v", err))
	}

	if !config.LinuxDoOAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "管理员未开启通过 LINUX DO 登录以及注册",
		})
		return
	}

	code := c.Query("code")
	linuxDoUser, err := getLinuxDoUserInfoByCode(code, c)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	user := model.User{
		LinuxDoId:         linuxDoUser.Id,
		LinuxDoUsername:   linuxDoUser.Username,
		LinuxDoTrustLevel: linuxDoUser.TrustLevel,
	}

	// 判断是否为已注册用户
	isExistingUser := model.IsLinuxDOIdAlreadyTaken(user.LinuxDoId)

	// 信任等级检查：动态限制开启或新用户注册时检查
	if config.LinuxDoOAuthTrustLevelEnabled {
		if config.LinuxDoOAuthDynamicTrustLevel || !isExistingUser {
			if linuxDoUser.TrustLevel < config.LinuxDoOAuthLowestTrustLevel {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "该 LINUX DO 信任等级过低，不允许访问",
				})
				return
			}
		}
	}

	// Check if user exists
	if isExistingUser {
		err := user.FillUserByLinuxDOId()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}

		// update linux do user trust level
		if user.LinuxDoTrustLevel != linuxDoUser.TrustLevel {
			updateTrustLevelUser := model.User{
				Id:                user.Id,
				LinuxDoTrustLevel: linuxDoUser.TrustLevel,
			}

			err = updateTrustLevelUser.Update(false)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				return
			}
		}

	} else {
		if config.RegisterEnabled {
			user.Username = "linuxdo_" + strconv.Itoa(model.GetMaxUserId()+1)
			if strings.TrimSpace(linuxDoUser.Name) == "" {
				user.DisplayName = linuxDoUser.Username
			} else {
				user.DisplayName = linuxDoUser.Name
			}
			user.Role = config.RoleCommonUser
			user.Status = config.UserStatusEnabled

			// 获取推荐码
			affCode := session.Get("aff")
			var affInviterId int
			if affCode != nil {
				affInviterId, _ = model.GetUserIdByAffCode(affCode.(string))
			}

			// 使用事务创建用户并处理邀请码
			err := model.DB.Transaction(func(tx *gorm.DB) error {
				// 验证和使用邀请码（如果启用）
				usedInviteCode, err := validateAndUseInviteCodeForOAuth(c, tx)
				if err != nil {
					return err
				}

				// 设置邀请人ID（使用原有推荐码逻辑）
				if affInviterId > 0 {
					user.InviterId = affInviterId
				}

				// 设置使用的邀请码
				if usedInviteCode != "" {
					user.UsedInviteCode = usedInviteCode
				}

				// 在事务中创建用户
				return user.InsertWithTx(tx, user.InviterId)
			})

			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": err.Error(),
				})
				return
			}
		} else {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "管理员关闭了新用户注册",
			})
			return
		}
	}

	if user.Status != config.UserStatusEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "用户已被封禁",
			"success": false,
		})
		return
	}

	setupLogin(&user, c)
}
