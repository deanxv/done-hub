package controller

import (
	"done-hub/common"
	"done-hub/model"
	"errors"
	"net/http"
	"net/url"

	"github.com/spf13/viper"

	"github.com/gin-gonic/gin"
)

func GetPricesList(c *gin.Context) {
	pricesType := c.DefaultQuery("type", "db")

	prices := model.GetPricesList(pricesType)

	if len(prices) == 0 {
		common.APIRespondWithError(c, http.StatusOK, errors.New("pricing data not found"))
		return
	}

	if pricesType == "old" {
		c.JSON(http.StatusOK, prices)
	} else {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data":    prices,
		})
	}
}

func GetAllModelList(c *gin.Context) {
	prices := model.PricingInstance.GetAllPrices()
	channelModel := model.ChannelGroup.Rule

	modelsMap := make(map[string]bool)
	for modelName := range prices {
		modelsMap[modelName] = true
	}

	for _, modelMap := range channelModel {
		for modelName := range modelMap {
			if _, ok := prices[modelName]; !ok {
				modelsMap[modelName] = true
			}
		}
	}

	var models []string
	for modelName := range modelsMap {
		models = append(models, modelName)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    models,
	})
}

func AddPrice(c *gin.Context) {
	var price model.Price
	if err := c.ShouldBindJSON(&price); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if err := model.PricingInstance.AddPrice(&price); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func UpdatePrice(c *gin.Context) {
	modelName := c.Param("model")
	if modelName == "" || len(modelName) < 2 {
		common.APIRespondWithError(c, http.StatusOK, errors.New("model name is required"))
		return
	}
	modelName = modelName[1:]
	modelName, _ = url.PathUnescape(modelName)

	var price model.Price
	if err := c.ShouldBindJSON(&price); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if err := model.PricingInstance.UpdatePrice(modelName, &price); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func DeletePrice(c *gin.Context) {
	modelName := c.Param("model")
	if modelName == "" || len(modelName) < 2 {
		common.APIRespondWithError(c, http.StatusOK, errors.New("model name is required"))
		return
	}
	modelName = modelName[1:]
	modelName, _ = url.PathUnescape(modelName)

	if err := model.PricingInstance.DeletePrice(modelName); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

type PriceBatchRequest struct {
	OriginalModels []string `json:"original_models"`
	model.BatchPrices
}

func BatchSetPrices(c *gin.Context) {
	pricesBatch := &PriceBatchRequest{}
	if err := c.ShouldBindJSON(pricesBatch); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if err := model.PricingInstance.BatchSetPrices(&pricesBatch.BatchPrices, pricesBatch.OriginalModels); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

type PriceBatchDeleteRequest struct {
	Models []string `json:"models" binding:"required"`
}

func BatchDeletePrices(c *gin.Context) {
	pricesBatch := &PriceBatchDeleteRequest{}
	if err := c.ShouldBindJSON(pricesBatch); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if err := model.PricingInstance.BatchDeletePrices(pricesBatch.Models); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func SyncPricing(c *gin.Context) {
	updateMode := c.DefaultQuery("updateMode", string(model.PriceUpdateModeSystem))

	prices := make([]*model.Price, 0)
	if err := c.ShouldBindJSON(&prices); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if len(prices) == 0 {
		common.APIRespondWithError(c, http.StatusOK, errors.New("prices is required"))
		return
	}

	err := model.PricingInstance.SyncPricing(prices, updateMode)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func GetUpdatePriceService(c *gin.Context) {
	updatePriceService := viper.GetString("update_price_service")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    updatePriceService,
		"message": "",
	})
}

// GetPricesFromModelsDev 从 models.dev/api.json 拉取并转换成 done-hub 的 []*Price 返回。
// 只做转换、不落库：前端拿到后走现有的检查更新预览，再由用户选模式调 /prices/sync 落库。
// 由后端拉取而非前端直连，是因为 models.dev 不保证 CORS，浏览器直连可能被拦。
func GetPricesFromModelsDev(c *gin.Context) {
	prices, err := model.GetPriceByModelsDev()
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    prices,
	})
}
