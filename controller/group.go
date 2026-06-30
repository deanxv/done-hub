package controller

import (
	"done-hub/common/logger"
	"done-hub/model"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type groupOption struct {
	Symbol      string  `json:"symbol"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Ratio       float64 `json:"ratio"`
}

func GetGroups(c *gin.Context) {
	userGroup := model.GlobalUserGroupRatio.GetAll()

	groups := make([]groupOption, 0, len(userGroup))
	for _, v := range userGroup {
		groups = append(groups, groupOption{
			Symbol:      v.Symbol,
			Name:        v.Name,
			Description: v.Description,
			Ratio:       v.Ratio,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    groups,
	})
}

type userGroupResponse struct {
	*model.UserGroup
	Inaccessible bool `json:"inaccessible,omitempty"`
}

func GetUserGroupRatio(c *gin.Context) {
	userId := c.GetInt("id")
	userSymbol := ""

	if userId > 0 {
		userSymbol, _ = model.CacheGetUserGroup(userId)
	}

	groupRatio := model.GlobalUserGroupRatio.GetAll()
	UserGroup := make(map[string]*userGroupResponse)
	for k, v := range groupRatio {
		if v.Public || k == userSymbol {
			UserGroup[k] = &userGroupResponse{UserGroup: v}
		}
	}

	// 补回当前用户 token 仍引用、但已不公开的分组，让前端能区分「未公开但 token 仍在引用」的分组
	if userId > 0 {
		tokenSymbols, err := model.GetUserTokenGroupSymbols(userId)
		if err != nil {
			logger.SysError(fmt.Sprintf("GetUserTokenGroupSymbols failed, userId=%d, err=%v", userId, err))
		} else {
			for _, sym := range tokenSymbols {
				if _, ok := UserGroup[sym]; ok {
					continue
				}
				if g, exists := groupRatio[sym]; exists {
					UserGroup[sym] = &userGroupResponse{UserGroup: g, Inaccessible: true}
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    UserGroup,
	})
}
