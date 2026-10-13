package controller

import (
	"done-hub/common"
	"done-hub/controller/check_channel"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// CheckChannel 对渠道的单个模型执行全部检测项。
// 一次请求只检测一个模型，由前端并发调度，与多模型测速的交互方式一致。
func CheckChannel(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	result, err := check_channel.CheckModel(channelId, c.Query("model"))
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}
