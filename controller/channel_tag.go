package controller

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/model"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

func GetChannelsTagList(c *gin.Context) {
	tag := c.Param("tag")
	if tag == "" {
		common.APIRespondWithError(c, http.StatusOK, errors.New("tag is required"))
		return
	}

	var params model.SearchChannelsTagParams
	if err := c.ShouldBindQuery(&params); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	// path param 为准，避免被 query 篡改
	params.Tag = tag

	channelsTag, err := model.GetChannelsTagList(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channelsTag,
	})
}

func GetChannelsTagAllList(c *gin.Context) {
	channelTags, err := model.GetChannelsTagAllList()
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channelTags,
	})
}

func GetChannelsTag(c *gin.Context) {
	tag := c.Param("tag")
	if tag == "" {
		common.AbortWithMessage(c, http.StatusOK, "tag is required")
		return
	}
	channel, err := model.GetChannelsTag(tag)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channel,
	})
}

func UpdateChannelsTag(c *gin.Context) {
	tag := c.Param("tag")
	if tag == "" {
		common.AbortWithMessage(c, http.StatusOK, "tag is required")
		return
	}
	channel := model.Channel{}
	err := c.ShouldBindJSON(&channel)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	err = model.UpdateChannelsTag(tag, &channel)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func DeleteChannelsTag(c *gin.Context) {
	tag := c.Param("tag")
	if tag == "" {
		common.AbortWithMessage(c, http.StatusOK, "tag is required")
		return
	}
	err := model.DeleteChannelsTag(tag, false)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func DeleteDisabledChannelsTag(c *gin.Context) {
	tag := c.Param("tag")
	if tag == "" {
		common.AbortWithMessage(c, http.StatusOK, "tag is required")
		return
	}
	err := model.DeleteChannelsTag(tag, true)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

// UpdateChannelsTagParams 承载整组「优先级 / 权重 / 成本倍率」的统一设置。
// Value 用 float64 以兼容成本倍率的小数；优先级/权重在落库前转回整型。
type UpdateChannelsTagParams struct {
	Type  string  `json:"type"`
	Value float64 `json:"value"`
}

func UpdateChannelsTagPriority(c *gin.Context) {
	tag := c.Param("tag")
	if tag == "" {
		common.AbortWithMessage(c, http.StatusOK, "tag is required")
		return
	}

	var params UpdateChannelsTagParams
	err := c.ShouldBindJSON(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	switch params.Type {
	case "priority":
		err = model.UpdateChannelsTagPriority(tag, int(params.Value))
	case "weight":
		if params.Value < 1 {
			params.Value = 1
		}
		err = model.UpdateChannelsTagWeight(tag, uint(params.Value))
	case "cost_ratio":
		if params.Value < 0 {
			params.Value = 0
		}
		err = model.UpdateChannelsTagCostRatio(tag, params.Value)
	default:
		common.AbortWithMessage(c, http.StatusOK, "invalid type")
		return
	}

	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func ChangeChannelsTagStatus(c *gin.Context) {
	tag := c.Param("tag")
	status := c.Param("status")
	if tag == "" {
		common.AbortWithMessage(c, http.StatusOK, "tag is required")
		return
	}

	var statusInt int
	switch status {
	case "enable":
		statusInt = config.ChannelStatusEnabled
	case "disable":
		statusInt = config.ChannelStatusManuallyDisabled
	}

	if statusInt == 0 {
		common.AbortWithMessage(c, http.StatusOK, "invalid status")
		return
	}

	err := model.ChangeChannelsTagStatus(tag, statusInt)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}
