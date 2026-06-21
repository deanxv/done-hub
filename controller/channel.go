package controller

import (
	"done-hub/common"
	"done-hub/common/utils"
	"done-hub/model"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func GetChannelsList(c *gin.Context) {
	var params model.SearchChannelsParams
	if err := c.ShouldBindQuery(&params); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	channels, err := model.GetChannelsList(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channels,
	})
}

func GetChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	channel, err := model.GetChannelById(id)
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
		"data":    channel,
	})
}

func AddChannel(c *gin.Context) {
	channel := model.Channel{}
	err := c.ShouldBindJSON(&channel)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if err = model.CheckTagTypeConsistency(channel.Tag, channel.Type, 0); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	channel.CreatedTime = utils.GetTimestamp()
	keys := strings.Split(channel.Key, "\n")

	baseUrls := []string{}
	if channel.BaseURL != nil && *channel.BaseURL != "" {
		baseUrls = strings.Split(*channel.BaseURL, "\n")
	}
	channels := make([]model.Channel, 0, len(keys))
	for index, key := range keys {
		if key == "" {
			continue
		}
		localChannel := channel
		localChannel.Key = key
		if index > 0 {
			localChannel.Name = localChannel.Name + "_" + strconv.Itoa(index+1)
		}

		if len(baseUrls) > index && baseUrls[index] != "" {
			localChannel.BaseURL = &baseUrls[index]
		} else if len(baseUrls) > 0 {
			localChannel.BaseURL = &baseUrls[0]
		}

		channels = append(channels, localChannel)
	}
	err = model.BatchInsertChannels(channels)
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

func DeleteChannel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	channel := model.Channel{Id: id}
	err := channel.Delete()
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

func DeleteChannelTag(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	err := model.DeleteChannelTag(id)
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

func DeleteDisabledChannel(c *gin.Context) {
	rows, err := model.DeleteDisabledChannel()
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
		"data":    rows,
	})
}

func UpdateChannel(c *gin.Context) {
	channel := model.Channel{}
	err := c.ShouldBindJSON(&channel)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	// 校验类型一致性：标签变化（加入/切换标签）时校验；标签未变但「在组内单独编辑改了类型」时
	// 也要校验，防止单渠道编辑把同一克隆组变成混合类型。仅编辑已有成员且类型未变则放行，
	// 避免阻断历史遗留的混合组成员的正常编辑。
	oldChannel, getErr := model.GetChannelById(channel.Id)
	if getErr != nil || oldChannel.Tag != channel.Tag || (channel.Tag != "" && oldChannel.Type != channel.Type) {
		if err = model.CheckTagTypeConsistency(channel.Tag, channel.Type, channel.Id); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
	}
	if channel.Models == "" {
		err = channel.Update(false)
	} else {
		err = channel.Update(true)
	}
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
		"data":    channel,
	})
}

func BatchUpdateChannelsAzureApi(c *gin.Context) {
	var params model.BatchChannelsParams
	err := c.ShouldBindJSON(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if params.Ids == nil || len(params.Ids) == 0 {
		common.APIRespondWithError(c, http.StatusOK, errors.New("ids不能为空"))
		return
	}
	var count int64
	count, err = model.BatchUpdateChannelsAzureApi(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    count,
		"success": true,
		"message": "更新成功",
	})
}

func BatchDelModelChannels(c *gin.Context) {
	var params model.BatchChannelsParams
	err := c.ShouldBindJSON(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if params.Ids == nil || len(params.Ids) == 0 {
		common.APIRespondWithError(c, http.StatusOK, errors.New("ids不能为空"))
		return
	}

	var count int64
	count, err = model.BatchDelModelChannels(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    count,
		"success": true,
		"message": "更新成功",
	})
}

func BatchAddUserGroupToChannels(c *gin.Context) {
	var params model.BatchChannelsParams
	err := c.ShouldBindJSON(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if params.Ids == nil || len(params.Ids) == 0 {
		common.APIRespondWithError(c, http.StatusOK, errors.New("ids不能为空"))
		return
	}

	if params.Value == "" {
		common.APIRespondWithError(c, http.StatusOK, errors.New("用户分组不能为空"))
		return
	}

	var count int64
	count, err = model.BatchAddUserGroupToChannels(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    count,
		"success": true,
		"message": "批量添加用户分组成功",
	})
}

func BatchAddModelToChannels(c *gin.Context) {
	var params model.BatchChannelsParams
	err := c.ShouldBindJSON(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if params.Ids == nil || len(params.Ids) == 0 {
		common.APIRespondWithError(c, http.StatusOK, errors.New("ids不能为空"))
		return
	}

	if params.Value == "" {
		common.APIRespondWithError(c, http.StatusOK, errors.New("模型不能为空"))
		return
	}

	var count int64
	count, err = model.BatchAddModelToChannels(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":    count,
		"success": true,
		"message": "批量添加模型成功",
	})
}

func BatchDeleteChannel(c *gin.Context) {
	var params model.BatchChannelsParams
	err := c.ShouldBindJSON(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if params.Ids == nil || len(params.Ids) == 0 {
		common.APIRespondWithError(c, http.StatusOK, errors.New("ids不能为空"))
		return
	}

	count, err := model.BatchDeleteChannel(params.Ids)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
}
