package relay

import (
	"done-hub/common"
	"done-hub/common/utils"
	"done-hub/model"
	"done-hub/providers/claude"
	"done-hub/providers/gemini"
	"done-hub/types"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/gin-gonic/gin"
)

// https://platform.openai.com/docs/api-reference/models/list
type OpenAIModels struct {
	Id      string  `json:"id"`
	Object  string  `json:"object"`
	Created int     `json:"created"`
	OwnedBy *string `json:"owned_by"`
}

// filterModelsByTokenLimit 根据令牌的模型限制过滤模型列表
func filterModelsByTokenLimit(c *gin.Context, models []string) []string {
	// 检查是否启用了模型限制
	tokenSetting, exists := c.Get("token_setting")
	if !exists {
		return models
	}

	setting, ok := tokenSetting.(*model.TokenSetting)
	if !ok || setting == nil {
		return models
	}

	if !setting.Limits.LimitModelSetting.Enabled {
		return models
	}

	if len(setting.Limits.LimitModelSetting.Models) == 0 {
		return models
	}

	// 如果启用了模型限制，只返回限制的模型列表
	limitedModels := setting.Limits.LimitModelSetting.Models
	// 创建一个map用于快速查找
	allowedModelsMap := make(map[string]bool, len(limitedModels))
	for _, m := range limitedModels {
		allowedModelsMap[m] = true
	}

	// 过滤模型列表，只保留允许的模型
	filteredModels := make([]string, 0, len(models))
	for _, modelName := range models {
		if allowedModelsMap[modelName] {
			filteredModels = append(filteredModels, modelName)
		}
	}

	return filteredModels
}

func ListModelsByToken(c *gin.Context) {
	groupName := c.GetString("token_group")
	if groupName == "" {
		groupName = c.GetString("group")
	}

	if groupName == "" {
		common.AbortWithMessage(c, http.StatusServiceUnavailable, "分组不存在")
		return
	}

	models, err := model.ChannelGroup.GetGroupModels(groupName)
	if err != nil {
		c.JSON(200, gin.H{
			"object": "list",
			"data":   []string{},
		})
		return
	}
	sort.Strings(models)

	// 根据令牌的模型限制过滤模型列表
	models = filterModelsByTokenLimit(c, models)

	var groupOpenAIModels []*OpenAIModels
	for _, modelName := range models {
		groupOpenAIModels = append(groupOpenAIModels, getOpenAIModelWithName(modelName))
	}

	// 根据 OwnedBy 排序
	sort.Slice(groupOpenAIModels, func(i, j int) bool {
		if groupOpenAIModels[i].OwnedBy == nil {
			return true
		}
		if groupOpenAIModels[j].OwnedBy == nil {
			return false
		}
		return *groupOpenAIModels[i].OwnedBy < *groupOpenAIModels[j].OwnedBy
	})

	c.JSON(200, gin.H{
		"object": "list",
		"data":   groupOpenAIModels,
	})
}

// https://generativelanguage.googleapis.com/v1beta/models?key=xxxxxxx
func ListGeminiModelsByToken(c *gin.Context) {
	groupName := c.GetString("token_group")
	if groupName == "" {
		groupName = c.GetString("group")
	}

	if groupName == "" {
		common.AbortWithMessage(c, http.StatusServiceUnavailable, "分组不存在")
		return
	}

	models, err := model.ChannelGroup.GetGroupModels(groupName)
	if err != nil {
		c.JSON(200, gemini.ModelListResponse{
			Models: []gemini.ModelDetails{},
		})
		return
	}
	sort.Strings(models)

	// 根据令牌的模型限制过滤模型列表
	models = filterModelsByTokenLimit(c, models)

	var geminiModels []gemini.ModelDetails
	for _, modelName := range models {
		geminiModels = append(geminiModels, gemini.ModelDetails{
			Name:        fmt.Sprintf("models/%s", modelName),
			DisplayName: cases.Title(language.Und).String(strings.ReplaceAll(modelName, "-", " ")),
			SupportedGenerationMethods: []string{
				"generateContent",
			},
		})
	}

	c.JSON(200, gemini.ModelListResponse{
		Models: geminiModels,
	})
}

func ListClaudeModelsByToken(c *gin.Context) {
	groupName := c.GetString("token_group")
	if groupName == "" {
		groupName = c.GetString("group")
	}

	if groupName == "" {
		common.AbortWithMessage(c, http.StatusServiceUnavailable, "分组不存在")
		return
	}

	models, err := model.ChannelGroup.GetGroupModels(groupName)
	if err != nil {
		c.JSON(200, claude.ModelListResponse{
			Data: []claude.Model{},
		})
		return
	}
	sort.Strings(models)

	// 根据令牌的模型限制过滤模型列表
	models = filterModelsByTokenLimit(c, models)

	var claudeModelsData []claude.Model
	for _, modelName := range models {
		claudeModelsData = append(claudeModelsData, claude.Model{
			ID:   modelName,
			Type: "model",
		})
	}

	c.JSON(200, claude.ModelListResponse{
		Data: claudeModelsData,
	})
}

func ListModelsForAdmin(c *gin.Context) {
	prices := model.PricingInstance.GetAllPrices()
	var openAIModels []OpenAIModels
	for modelId, price := range prices {
		openAIModels = append(openAIModels, OpenAIModels{
			Id:      modelId,
			Object:  "model",
			Created: 1677649963,
			OwnedBy: getModelOwnedBy(price.ChannelType),
		})
	}
	// 根据 OwnedBy 排序
	sort.Slice(openAIModels, func(i, j int) bool {
		if openAIModels[i].OwnedBy == nil {
			return true // 假设 nil 值小于任何非 nil 值
		}
		if openAIModels[j].OwnedBy == nil {
			return false // 假设任何非 nil 值大于 nil 值
		}
		return *openAIModels[i].OwnedBy < *openAIModels[j].OwnedBy
	})

	c.JSON(200, gin.H{
		"object": "list",
		"data":   openAIModels,
	})
}

func RetrieveModel(c *gin.Context) {
	modelName := c.Param("model")
	openaiModel := getOpenAIModelWithName(modelName)
	if *openaiModel.OwnedBy != model.UnknownOwnedBy {
		c.JSON(200, openaiModel)
	} else {
		openAIError := types.OpenAIError{
			Message: fmt.Sprintf("The model '%s' does not exist", modelName),
			Type:    "invalid_request_error",
			Param:   "model",
			Code:    "model_not_found",
		}
		c.JSON(200, gin.H{
			"error": openAIError,
		})
	}
}

func getModelOwnedBy(channelType int) (ownedBy *string) {
	ownedByName := model.ModelOwnedBysInstance.GetName(channelType)
	if ownedByName != "" {
		return &ownedByName
	}

	return &model.UnknownOwnedBy
}

func getOpenAIModelWithName(modelName string) *OpenAIModels {
	price := model.PricingInstance.GetPrice(modelName)

	return &OpenAIModels{
		Id:      modelName,
		Object:  "model",
		Created: 1677649963,
		OwnedBy: getModelOwnedBy(price.ChannelType),
	}
}

func GetModelOwnedBy(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    model.ModelOwnedBysInstance.GetAll(),
	})
}

type ModelPrice struct {
	Type   string  `json:"type"`
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

type AvailableModelResponse struct {
	Groups  []string     `json:"groups"`
	OwnedBy string       `json:"owned_by"`
	Price   *model.Price `json:"price"`
}

func AvailableModel(c *gin.Context) {
	groupName := c.GetString("group")

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    getAvailableModels(groupName),
	})
}

func GetAvailableModels(groupName string) map[string]*AvailableModelResponse {
	return getAvailableModels(groupName)
}

func getAvailableModels(groupName string) map[string]*AvailableModelResponse {
	publicModels := model.ChannelGroup.GetModelsGroups()
	publicGroups := model.GlobalUserGroupRatio.GetPublicGroupList()
	if groupName != "" && !utils.Contains(groupName, publicGroups) {
		publicGroups = append(publicGroups, groupName)
	}

	availableModels := make(map[string]*AvailableModelResponse, len(publicModels))

	for modelName, group := range publicModels {
		groups := []string{}
		for _, publicGroup := range publicGroups {
			if group[publicGroup] {
				groups = append(groups, publicGroup)
			}
		}

		if len(groups) == 0 {
			continue
		}

		if _, ok := availableModels[modelName]; !ok {
			price := model.PricingInstance.GetPrice(modelName)
			availableModels[modelName] = &AvailableModelResponse{
				Groups:  groups,
				OwnedBy: *getModelOwnedBy(price.ChannelType),
				Price:   price,
			}
		}
	}

	return availableModels
}
