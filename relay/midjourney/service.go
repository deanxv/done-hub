// Author: Calcium-Ion
// GitHub: https://github.com/Calcium-Ion/new-api
// Path: service/midjourney.go
package midjourney

import (
	mjProvider "done-hub/providers/midjourney"
	"strconv"
	"strings"
)

func CoverActionToModelName(mjAction, model string) string {
	if model == "fast" {
		model = ""
	}

	if model != "" {
		model = model + "_"
	}

	modelName := "mj_" + model + strings.ToLower(mjAction)
	return modelName
}

func GetMjRequestModel(relayMode int, midjRequest *mjProvider.MidjourneyRequest, mjModel string) (string, *mjProvider.MidjourneyResponse, bool) {
	action := ""
	if relayMode == mjProvider.RelayModeMidjourneyAction {
		// plus request
		err := CoverPlusActionToNormalAction(midjRequest)
		if err != nil {
			return "", err, false
		}
		action = midjRequest.Action
	} else {
		switch relayMode {
		case mjProvider.RelayModeMidjourneyImagine:
			action = mjProvider.MjActionImagine
		case mjProvider.RelayModeMidjourneyDescribe:
			action = mjProvider.MjActionDescribe
		case mjProvider.RelayModeMidjourneyBlend:
			action = mjProvider.MjActionBlend
		case mjProvider.RelayModeMidjourneyShorten:
			action = mjProvider.MjActionShorten
		case mjProvider.RelayModeMidjourneyChange:
			action = midjRequest.Action
		case mjProvider.RelayModeMidjourneyModal:
			action = mjProvider.MjActionModal
		case mjProvider.RelayModeMidjourneySwapFace:
			action = mjProvider.MjActionSwapFace
		case mjProvider.RelayModeMidjourneyUpload:
			action = mjProvider.MjActionUpload
		case mjProvider.RelayModeMidjourneySimpleChange:
			params := ConvertSimpleChangeParams(midjRequest.Content)
			if params == nil {
				return "", mjProvider.MidjourneyErrorWrapper(mjProvider.MjRequestError, "invalid_request"), false
			}
			action = params.Action
		case mjProvider.RelayModeMidjourneyTaskFetch, mjProvider.RelayModeMidjourneyTaskFetchByCondition, mjProvider.RelayModeMidjourneyNotify:
			return "", nil, true
		default:
			return "", mjProvider.MidjourneyErrorWrapper(mjProvider.MjRequestError, "unknown_relay_action"), false
		}
	}

	modelName := CoverActionToModelName(action, mjModel)
	return modelName, nil, true
}

// customIdIndexSegment 是 customId 中承载图格索引的段位下标，
// 形如 "MJ::JOB::upsample::2::<taskId>" 中的 "2"。
const customIdIndexSegment = 3

// parseCustomIdIndex 取出 customId 的索引段。customId 来自客户端可控入参，
// 段数不足时必须当作请求错误返回，不能直接下标访问（历史上会 panic 成 500）。
func parseCustomIdIndex(splits []string) (int, *mjProvider.MidjourneyResponse) {
	if len(splits) <= customIdIndexSegment {
		return 0, mjProvider.MidjourneyErrorWrapper(mjProvider.MjRequestError, "index_parse_failed")
	}
	index, err := strconv.Atoi(splits[customIdIndexSegment])
	if err != nil {
		return 0, mjProvider.MidjourneyErrorWrapper(mjProvider.MjRequestError, "index_parse_failed")
	}
	return index, nil
}

func CoverPlusActionToNormalAction(midjRequest *mjProvider.MidjourneyRequest) *mjProvider.MidjourneyResponse {
	// "customId": "MJ::JOB::upsample::2::3dbbd469-36af-4a0f-8f02-df6c579e7011"
	customId := midjRequest.CustomId
	if customId == "" {
		return mjProvider.MidjourneyErrorWrapper(mjProvider.MjRequestError, "custom_id_is_required")
	}
	splits := strings.Split(customId, "::")
	// 至少需要 2 段才能判定 action 所在位置；不足则是畸形 customId，
	// 早返回避免后续下标越界（strings.Split 永不返回空切片，len>=1）。
	if len(splits) < 2 {
		return mjProvider.MidjourneyErrorWrapper(mjProvider.MjRequestError, "unknown_action:"+customId)
	}
	var action string
	if splits[1] == "JOB" {
		if len(splits) < 3 {
			return mjProvider.MidjourneyErrorWrapper(mjProvider.MjRequestError, "unknown_action:"+customId)
		}
		action = splits[2]
	} else {
		action = splits[1]
	}

	if action == "" {
		return mjProvider.MidjourneyErrorWrapper(mjProvider.MjRequestError, "unknown_action")
	}
	if strings.Contains(action, "upsample") {
		index, mjErr := parseCustomIdIndex(splits)
		if mjErr != nil {
			return mjErr
		}
		midjRequest.Index = index
		midjRequest.Action = mjProvider.MjActionUpscale
	} else if strings.Contains(action, "variation") {
		midjRequest.Index = 1
		if action == "variation" {
			index, mjErr := parseCustomIdIndex(splits)
			if mjErr != nil {
				return mjErr
			}
			midjRequest.Index = index
			midjRequest.Action = mjProvider.MjActionVariation
		} else if action == "low_variation" {
			midjRequest.Action = mjProvider.MjActionLowVariation
		} else if action == "high_variation" {
			midjRequest.Action = mjProvider.MjActionHighVariation
		}
	} else if strings.Contains(action, "pan") {
		midjRequest.Action = mjProvider.MjActionPan
		midjRequest.Index = 1
	} else if strings.Contains(action, "reroll") {
		midjRequest.Action = mjProvider.MjActionReRoll
		midjRequest.Index = 1
	} else if action == "Outpaint" {
		midjRequest.Action = mjProvider.MjActionZoom
		midjRequest.Index = 1
	} else if action == "CustomZoom" {
		midjRequest.Action = mjProvider.MjActionCustomZoom
		midjRequest.Index = 1
	} else if action == "Inpaint" {
		midjRequest.Action = mjProvider.MjActionInPaint
		midjRequest.Index = 1
	} else {
		return mjProvider.MidjourneyErrorWrapper(mjProvider.MjRequestError, "unknown_action:"+customId)
	}
	return nil
}

func ConvertSimpleChangeParams(content string) *mjProvider.MidjourneyRequest {
	split := strings.Split(content, " ")
	if len(split) != 2 {
		return nil
	}

	action := strings.ToLower(split[1])

	changeParams := &mjProvider.MidjourneyRequest{}
	changeParams.TaskId = split[0]

	if action == "r" {
		changeParams.Action = "REROLL"
		return changeParams
	}

	// content 是客户端可控入参，"taskId " / "taskId u" 这类残缺动作会让下面的
	// action[0]、action[1:2] 越界 panic（经 recover 中间件表现为 500）。
	// 除 "r" 外合法动作都是 "u<n>" / "v<n>" 两字符，这里先把长度卡住。
	if len(action) < 2 {
		return nil
	}

	if action[0] == 'u' {
		changeParams.Action = "UPSCALE"
	} else if action[0] == 'v' {
		changeParams.Action = "VARIATION"
	} else {
		return nil
	}

	index, err := strconv.Atoi(action[1:2])
	if err != nil || index < 1 || index > 4 {
		return nil
	}
	changeParams.Index = index
	return changeParams
}
