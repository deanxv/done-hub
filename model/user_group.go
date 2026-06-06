package model

import (
	"done-hub/common/config"
	"done-hub/common/limit"
	"done-hub/common/logger"
	"fmt"
	"sync"
)

type UserGroup struct {
	Id          int     `json:"id"`
	Symbol      string  `json:"symbol" gorm:"type:varchar(50);uniqueIndex"`
	Name        string  `json:"name" gorm:"type:varchar(50)"`
	Description string  `json:"description" gorm:"type:varchar(500)"`            // 分组描述，展示给用户看
	Ratio       float64 `json:"ratio" gorm:"type:decimal(10,2); default:1"`      // 倍率
	APIRate     int     `json:"api_rate" gorm:"default:600"`                     // 每分组允许的请求数
	Public      bool    `json:"public" form:"public" gorm:"default:false"`       // 是否为公开分组，如果是，则可以被用户在令牌中选择
	Promotion   bool    `json:"promotion" form:"promotion" gorm:"default:false"` // 是否是自动升级用户组， 如果是则用户充值金额满足条件自动升级
	Min         int     `json:"min" form:"min" gorm:"default:0"`                 // 晋级条件最小值
	Max         int     `json:"max" form:"max" gorm:"default:0"`                 // 晋级条件最大值
	Enable      *bool   `json:"enable" form:"enable" gorm:"default:true"`        // 是否启用
}

type SearchUserGroupParams struct {
	UserGroup
	PaginationParams
}

var allowedUserGroupOrderFields = map[string]bool{
	"id":     true,
	"name":   true,
	"enable": true,
}

func GetUserGroupsList(params *SearchUserGroupParams) (*DataResult[UserGroup], error) {
	var userGroups []*UserGroup
	db := DB

	if params.Name != "" {
		db = db.Where("name LIKE ?", params.Name+"%")
	}

	if params.Enable != nil {
		db = db.Where("enable = ?", *params.Enable)
	}

	return PaginateAndOrder(db, &params.PaginationParams, &userGroups, allowedUserGroupOrderFields)
}

func GetUserGroupsById(id int) (*UserGroup, error) {
	var userGroup UserGroup
	err := DB.Where("id = ?", id).First(&userGroup).Error
	return &userGroup, err
}

func (c *UserGroup) Create() error {
	// 确保enable字段有默认值
	if c.Enable == nil {
		enable := true
		c.Enable = &enable
	}
	err := DB.Create(c).Error
	if err == nil {
		GlobalUserGroupRatio.Load()
	}
	return err
}

func (c *UserGroup) Update() error {
	err := DB.Select("name", "description", "ratio", "public", "api_rate", "promotion", "min", "max").Updates(c).Error
	if err == nil {
		GlobalUserGroupRatio.Load()
	}

	return err
}

func (c *UserGroup) Delete() error {
	err := DB.Delete(c).Error

	if err == nil {
		GlobalUserGroupRatio.Load()
	}
	return err
}

func ChangeUserGroupEnable(id int, enable bool) error {
	err := DB.Model(&UserGroup{}).Where("id = ?", id).Update("enable", enable).Error
	if err == nil {
		GlobalUserGroupRatio.Load()
	}
	return err
}

type groupNameEntry struct {
	Name    string
	Enabled bool
}

type UserGroupRatio struct {
	sync.RWMutex
	UserGroup     map[string]*UserGroup
	APILimiter    map[string]limit.RateLimiter
	PublicGroup   []string
	AllGroupNames map[string]groupNameEntry // 含禁用分组，供 GetDisplayName / GetDisplayNameWithStatus 展示
}

var GlobalUserGroupRatio = UserGroupRatio{}

func (cgrm *UserGroupRatio) Load() {
	// 单次全量查询后本地分桶，避免分两次查 enabled/disabled 之间的时序窗口（先前实现的遗留），
	// 同时也减少 DB 调用。整体加载失败维持旧缓存（不替换字段）。
	var allGroups []*UserGroup
	if err := DB.Find(&allGroups).Error; err != nil {
		logger.SysError(fmt.Sprintf("UserGroupRatio load failed, keep previous cache: %v", err))
		return
	}

	newUserGroups := make(map[string]*UserGroup, len(allGroups))
	newAPILimiter := make(map[string]limit.RateLimiter, len(allGroups))
	publicGroup := make([]string, 0)
	newAllNames := make(map[string]groupNameEntry, len(allGroups))

	for _, g := range allGroups {
		enabled := g.Enable != nil && *g.Enable
		newAllNames[g.Symbol] = groupNameEntry{Name: g.Name, Enabled: enabled}
		if !enabled {
			continue
		}
		newUserGroups[g.Symbol] = g
		newAPILimiter[g.Symbol] = limit.NewAPILimiter(g.APIRate)
		if g.Public {
			publicGroup = append(publicGroup, g.Symbol)
		}
	}

	cgrm.Lock()
	defer cgrm.Unlock()

	cgrm.UserGroup = newUserGroups
	cgrm.APILimiter = newAPILimiter
	cgrm.PublicGroup = publicGroup
	cgrm.AllGroupNames = newAllNames
}

func (cgrm *UserGroupRatio) GetBySymbol(symbol string) *UserGroup {
	cgrm.RLock()
	defer cgrm.RUnlock()

	if symbol == "" {
		return nil
	}

	userGroupRatio, ok := cgrm.UserGroup[symbol]
	if !ok {
		return nil
	}

	return userGroupRatio
}

func (cgrm *UserGroupRatio) GetByTokenUserGroup(tokenGroup, userGroup string) *UserGroup {
	if tokenGroup != "" {
		return cgrm.GetBySymbol(tokenGroup)
	}

	return cgrm.GetBySymbol(userGroup)
}

func (cgrm *UserGroupRatio) GetAll() map[string]*UserGroup {
	cgrm.RLock()
	defer cgrm.RUnlock()

	return cgrm.UserGroup
}

func (cgrm *UserGroupRatio) GetAPIRate(symbol string) int {
	userGroup := cgrm.GetBySymbol(symbol)
	if userGroup == nil {
		return 0
	}

	return userGroup.APIRate
}

// GetDisplayName 返回分组展示名（启用/禁用都只返回 name），name 为空或分组被物理删除时 fallback 到 symbol。
// 用于错误模板等对外/通用文案，保持纯文本输出，避免污染日志关键字告警的正则匹配。
// 需要在日志里区分禁用状态时，用 GetDisplayNameWithStatus。
func (cgrm *UserGroupRatio) GetDisplayName(symbol string) string {
	cgrm.RLock()
	defer cgrm.RUnlock()
	if entry, ok := cgrm.AllGroupNames[symbol]; ok && entry.Name != "" {
		return entry.Name
	}
	return symbol
}

// GetDisplayNameWithStatus 返回带状态的展示名：禁用分组拼上"（已禁用）"，便于 admin 在错误日志里
// 一眼定位"分组被误禁导致路由失败"的配置异常。仅用于内部日志诊断，不要用于对外文案。
func (cgrm *UserGroupRatio) GetDisplayNameWithStatus(symbol string) string {
	cgrm.RLock()
	defer cgrm.RUnlock()
	if entry, ok := cgrm.AllGroupNames[symbol]; ok && entry.Name != "" {
		if !entry.Enabled {
			return entry.Name + "（已禁用）"
		}
		return entry.Name
	}
	return symbol
}

func (cgrm *UserGroupRatio) GetPublicGroupList() []string {
	cgrm.RLock()
	defer cgrm.RUnlock()

	return cgrm.PublicGroup
}

func (cgrm *UserGroupRatio) GetAPILimiter(symbol string) limit.RateLimiter {
	cgrm.RLock()
	defer cgrm.RUnlock()

	limiter, ok := cgrm.APILimiter[symbol]
	if !ok {
		return nil
	}

	return limiter
}

// CheckAndUpgradeUserGroup checks if a user's cumulative recharge amount falls within any promotion group's range
// and upgrades the user to that group if a match is found.
// The cumulative recharge amount is calculated as Quota + UsedQuota + rechargeAmount.
func CheckAndUpgradeUserGroup(userId int, rechargeAmount int) error {
	// Get user's current quota and used quota
	user := &User{}
	err := DB.Where("id = ?", userId).First(user).Error
	if err != nil {
		return err
	}

	// Calculate cumulative recharge amount
	cumulativeAmount := user.Quota + user.UsedQuota + rechargeAmount
	logger.SysError(fmt.Sprintf("use:%f q:%f  cumulative:%f rechargeAmount:%f", (float64)(user.UsedQuota)/config.QuotaPerUnit, (float64)(user.Quota)/config.QuotaPerUnit, (float64)(cumulativeAmount)/config.QuotaPerUnit, (float64)(rechargeAmount)/config.QuotaPerUnit))
	// Get all promotion-enabled user groups
	var promotionGroups []*UserGroup
	err = DB.Where("promotion = ? AND enable = ?", true, true).Find(&promotionGroups).Error
	if err != nil {
		return err
	}

	// Find a matching group (min <= cumulativeAmount < max)
	var targetGroup *UserGroup
	for _, group := range promotionGroups {
		var minQuota = (float64)(group.Min) * config.QuotaPerUnit
		var maxQuota = (float64)(group.Max) * config.QuotaPerUnit
		if (float64)(cumulativeAmount) >= minQuota && (group.Max == 0 || (float64)(cumulativeAmount) < maxQuota) {
			// If multiple groups match, choose the one with higher min value
			if targetGroup == nil || group.Min > targetGroup.Min {
				targetGroup = group
			}
		}
	}

	// If a matching group is found, upgrade the user
	if targetGroup != nil && targetGroup.Symbol != user.Group {
		// Update user's group
		err = DB.Model(&User{}).Where("id = ?", userId).Update("group", targetGroup.Symbol).Error
		if err != nil {
			return err
		}

		// Delete cache if Redis is enabled
		ClearUserGroupAndTokensCache(userId)
	}

	return nil
}
