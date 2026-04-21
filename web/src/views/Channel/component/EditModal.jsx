import PropTypes from 'prop-types'
import { useEffect, useRef, useState } from 'react'
import { CHANNEL_OPTIONS } from 'constants/ChannelConstants'
import { useTheme } from '@mui/material/styles'
import { API } from 'utils/api'
import { copy, showError, showSuccess, trims } from 'utils/common'
import {
  Alert,
  Autocomplete,
  Box,
  Button,
  ButtonGroup,
  Checkbox,
  Chip,
  Collapse,
  Container,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControl,
  FormControlLabel,
  FormHelperText,
  IconButton,
  InputLabel,
  MenuItem,
  OutlinedInput,
  Select,
  Stack,
  Switch,
  TextField,
  Tooltip,
  Typography,
  useMediaQuery
} from '@mui/material'
import { Formik } from 'formik'
import * as Yup from 'yup'
import { defaultConfig, typeConfig } from '../type/Config' //typeConfig
import { createFilterOptions } from '@mui/material/Autocomplete'
import CheckBoxOutlineBlankIcon from '@mui/icons-material/CheckBoxOutlineBlank'
import CheckBoxIcon from '@mui/icons-material/CheckBox'
import { useTranslation } from 'react-i18next'
import useCustomizeT from 'hooks/useCustomizeT'
import { PreCostType } from '../type/other'
import MapInput from './MapInput'
import ListInput from './ListInput'
import ModelSelectorModal from './ModelSelectorModal'
import pluginList from '../type/Plugin.json'
import { Icon } from '@iconify/react'
import Editor from '@monaco-editor/react'

const icon = <CheckBoxOutlineBlankIcon fontSize="small"/>
const checkedIcon = <CheckBoxIcon fontSize="small"/>

const filter = createFilterOptions()
const getValidationSchema = (t) =>
  Yup.object().shape({
    is_edit: Yup.boolean(),
    // is_tag: Yup.boolean(),
    name: Yup.string().required(t('channel_edit.requiredName')),
    type: Yup.number().required(t('channel_edit.requiredChannel')),
    key: Yup.string().when('is_edit', { is: false, then: Yup.string().required(t('channel_edit.requiredKey')) }),
    other: Yup.string(),
    remark: Yup.string(),
    proxy: Yup.string(),
    test_model: Yup.string(),
    models: Yup.array().min(1, t('channel_edit.requiredModels')),
    groups: Yup.array().min(1, t('channel_edit.requiredGroup')),
    base_url: Yup.string().when('type', {
      is: (value) => [3, 8].includes(value),
      then: Yup.string().required(t('channel_edit.requiredBaseUrl')), // base_url 是必需的
      otherwise: Yup.string() // 在其他情况下，base_url 可以是任意字符串
    }),
    model_mapping: Yup.array(),
    model_headers: Yup.array(),
    custom_parameter: Yup.string().nullable()
  })

const EditModal = ({ open, channelId, onCancel, onOk, groupOptions, isTag, modelOptions, prices }) => {
  const { t } = useTranslation()
  const { t: customizeT } = useCustomizeT()
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'))
  // const [loading, setLoading] = useState(false);
  const [initialInput, setInitialInput] = useState(defaultConfig.input)
  const [inputLabel, setInputLabel] = useState(defaultConfig.inputLabel) //
  const [inputPrompt, setInputPrompt] = useState(defaultConfig.prompt)
  const [batchAdd, setBatchAdd] = useState(false)
  const [hasTag, setHasTag] = useState(false)
  const [expanded, setExpanded] = useState(false)
  const [inputValue, setInputValue] = useState('')
  const [parameterFocused, setParameterFocused] = useState(false)
  const parameterInputRef = useRef(null)
  const removeDuplicates = (array) => [...new Set(array)]
  const [modelSelectorOpen, setModelSelectorOpen] = useState(false)
  const [tempFormikValues, setTempFormikValues] = useState(null)
  const [tempSetFieldValue, setTempSetFieldValue] = useState(null)

  // 用于追踪模型的原始名称映射关系 { displayName: originalName }
  const [modelOriginalMapping, setModelOriginalMapping] = useState({})

  // GeminiCli OAuth 相关状态
  const [oauthLoading, setOauthLoading] = useState(false)
  const [oauthWindow, setOauthWindow] = useState(null)
  const [oauthState, setOauthState] = useState(null)
  const [oauthURL, setOauthURL] = useState('')
  const oauthHandledRef = useRef(false) // 用于防止重复处理
  const pollingIntervalRef = useRef(null) // 使用 ref 存储 interval ID

  // ClaudeCode OAuth 相关状态
  const [claudeCodeOAuthVisible, setClaudeCodeOAuthVisible] = useState(false)
  const [claudeCodeAuthURL, setClaudeCodeAuthURL] = useState('')
  const [claudeCodeSessionId, setClaudeCodeSessionId] = useState('')
  const [claudeCodeAuthCode, setClaudeCodeAuthCode] = useState('')
  const [claudeCodeSubmitting, setClaudeCodeSubmitting] = useState(false)

  // Codex OAuth 相关状态
  const [codexOAuthVisible, setCodexOAuthVisible] = useState(false)
  const [codexAuthURL, setCodexAuthURL] = useState('')
  const [codexSessionId, setCodexSessionId] = useState('')
  const [codexAuthCode, setCodexAuthCode] = useState('')
  const [codexSubmitting, setCodexSubmitting] = useState(false)

  // 清理 OAuth 相关资源
  const cleanupOAuth = () => {
    if (pollingIntervalRef.current) {
      clearInterval(pollingIntervalRef.current)
      pollingIntervalRef.current = null
    }
    if (oauthWindow && !oauthWindow.closed) {
      oauthWindow.close()
    }
    setOauthWindow(null)
    setOauthLoading(false)
    setOauthState(null)
    oauthHandledRef.current = false
  }

  // 包装 onCancel，添加清理逻辑
  const handleCancel = () => {
    cleanupOAuth()
    onCancel()
  }

  const initChannel = (typeValue) => {
    if (typeConfig[typeValue]?.inputLabel) {
      setInputLabel({ ...defaultConfig.inputLabel, ...typeConfig[typeValue].inputLabel })
    } else {
      setInputLabel(defaultConfig.inputLabel)
    }

    if (typeConfig[typeValue]?.prompt) {
      setInputPrompt({ ...defaultConfig.prompt, ...typeConfig[typeValue].prompt })
    } else {
      setInputPrompt(defaultConfig.prompt)
    }

    return typeConfig[typeValue]?.input
  }

  // 解析模型映射配置的工具函数
  const parseModelMapping = (mappingArray) => {
    if (!mappingArray || !Array.isArray(mappingArray) || mappingArray.length === 0) {
      return null
    }

    try {
      const mapping = mappingArray.reduce((acc, item) => {
        if (item.key && item.value) {
          acc[item.key] = item.value
        }
        return acc
      }, {})

      if (Object.keys(mapping).length === 0) {
        return null
      }
      return mapping
    } catch (error) {
      console.warn('模型重定向解析失败:', error)
      return null
    }
  }

  // 更新模型列表的统一方法
  const updateModelsList = (newModels, newMapping, setFieldValue) => {
    const uniqueModels = Array.from(new Set(newModels.filter(model => model && model.id && model.id.trim())))

    setFieldValue('models', uniqueModels)
    setModelOriginalMapping(newMapping)
  }

  // 恢复模型到原始名称
  const restoreModelsToOriginalNames = (currentModels, setFieldValue) => {
    const restoredModels = currentModels.map(model => {
      const originalName = modelOriginalMapping[model.id] || model.id
      return {
        ...model,
        id: originalName
      }
    })

    // 检查是否有变化
    const hasChanges = currentModels.some((model, index) => {
      return model.id !== restoredModels[index].id
    })

    if (hasChanges) {
      updateModelsList(restoredModels, {}, setFieldValue)
    }
  }

  // 应用模型映射的核心逻辑
  const applyModelMapping = (mapping, currentModels, currentMapping, setFieldValue) => {
    let updatedModels = [...currentModels]
    let newMapping = { ...currentMapping }
    let hasChanges = false

    // 遍历重定向映射
    Object.entries(mapping).forEach(([key, mappedValue]) => {
      if (typeof key === 'string' && typeof mappedValue === 'string') {
        const keyTrimmed = key.trim()
        const valueTrimmed = mappedValue.trim()

        if (keyTrimmed && valueTrimmed) {
          // 查找模型配置中是否存在重定向的"值"（原始模型名）
          const valueIndex = updatedModels.findIndex(model => {
            return model.id === valueTrimmed || newMapping[model.id] === valueTrimmed
          })

          if (valueIndex !== -1) {
            const currentDisplayName = updatedModels[valueIndex].id
            if (currentDisplayName !== keyTrimmed) {
              // 记录原始映射关系
              if (!newMapping[keyTrimmed]) {
                newMapping[keyTrimmed] = newMapping[currentDisplayName] || currentDisplayName
              }
              // 清理旧的映射关系
              if (newMapping[currentDisplayName]) {
                delete newMapping[currentDisplayName]
              }
              // 更新显示名称为重定向的键
              updatedModels[valueIndex] = {
                ...updatedModels[valueIndex],
                id: keyTrimmed
              }
              hasChanges = true
            }
          }
        }
      }
    })

    // 处理不在映射中的模型，恢复为原始名称
    const mappingKeys = new Set(Object.keys(mapping).map(key => key.trim()))
    updatedModels = updatedModels.map(model => {
      if (!mappingKeys.has(model.id) && newMapping[model.id]) {
        const originalName = newMapping[model.id]
        delete newMapping[model.id]
        hasChanges = true
        return {
          ...model,
          id: originalName
        }
      }
      return model
    })

    return { updatedModels, newMapping, hasChanges }
  }

  // 实时同步模型重定向到模型配置的函数
  const syncModelMappingToModels = (mappingArray, currentModels, setFieldValue) => {
    const mapping = parseModelMapping(mappingArray)

    if (!mapping) {
      restoreModelsToOriginalNames(currentModels, setFieldValue)
      return
    }

    const { updatedModels, newMapping, hasChanges } = applyModelMapping(
      mapping,
      currentModels,
      modelOriginalMapping,
      setFieldValue
    )

    if (hasChanges) {
      updateModelsList(updatedModels, newMapping, setFieldValue)
    }
  }

  // 轮询 OAuth 状态
  const pollOAuthStatus = async(state, setFieldValue, messageHandlerRef, channelType = 57) => {
    try {
      // 最早检查：如果已经处理过，立即返回，不做任何操作
      if (oauthHandledRef.current) {
        return true
      }

      // 根据渠道类型选择 API 端点
      const apiEndpoint = channelType === 60 ? 'antigravity' : 'geminicli'
      const res = await API.get(`/api/${apiEndpoint}/oauth/status/${state}`)

      if (!res.data.success) {
        return false
      }

      if (res.data.status === 'completed') {
        // 再次检查，防止竞态条件
        if (oauthHandledRef.current) {
          return true
        }

        // 立即设置标志，防止其他路径重复处理
        oauthHandledRef.current = true

        // 立即停止轮询
        if (pollingIntervalRef.current) {
          clearInterval(pollingIntervalRef.current)
          pollingIntervalRef.current = null
        }

        // 移除 message 监听器
        if (messageHandlerRef && messageHandlerRef.current) {
          window.removeEventListener('message', messageHandlerRef.current)
          messageHandlerRef.current = null
        }

        // 更新状态
        setOauthLoading(false)
        setOauthState(null)

        // 处理结果
        if (res.data.result && res.data.credentials) {
          setFieldValue('key', res.data.credentials)
          showSuccess('OAuth 授权成功！凭证已自动填充')
        } else {
          showError(res.data.message || 'OAuth 授权失败')
        }

        // 关闭弹窗
        if (oauthWindow && !oauthWindow.closed) {
          oauthWindow.close()
        }
        setOauthWindow(null)

        return true // 已完成
      }

      return false // 未完成
    } catch (error) {
      return false
    }
  }

  // 复制 OAuth 授权链接
  const handleCopyOAuthURL = () => {
    if (!oauthURL) {
      showError('请先点击授权按钮生成授权链接')
      return
    }
    navigator.clipboard.writeText(oauthURL).then(() => {
      showSuccess('授权链接已复制到剪贴板')
    }).catch(() => {
      showError('复制失败，请手动复制')
    })
  }

  // GeminiCli/Antigravity OAuth 授权处理
  const handleGeminiCliOAuth = async(projectId, proxy, setFieldValue, channelType = 57) => {
    // 允许 projectId 为空，支持自动检测
    const trimmedProjectId = projectId ? projectId.trim() : ''
    const trimmedProxy = proxy ? proxy.trim() : ''

    // 根据渠道类型选择 API 端点和名称
    const apiEndpoint = channelType === 60 ? 'antigravity' : 'geminicli'
    const channelName = channelType === 60 ? 'Antigravity' : 'GeminiCli'

    try {
      setOauthLoading(true)
      oauthHandledRef.current = false // 重置处理标志

      // 调用后端 API 生成授权 URL（传递代理配置）
      const res = await API.post(`/api/${apiEndpoint}/oauth/start`, {
        channel_id: channelId || 0,
        project_id: trimmedProjectId,
        proxy: trimmedProxy
      })

      if (!res.data.success) {
        showError(res.data.message || 'OAuth 授权失败')
        setOauthLoading(false)
        return
      }

      const authURL = res.data.auth_url
      const state = res.data.state

      setOauthState(state)
      setOauthURL(authURL)

      // 打开新窗口进行授权（使用新标签页）
      const popup = window.open(authURL, '_blank')

      setOauthWindow(popup)

      // 用于存储 message handler 的引用
      const messageHandlerRef = { current: null }

      // 监听来自 OAuth 窗口的消息（作为快速路径）
      const handleMessage = (event) => {
        // 安全检查：确保消息来自我们的域（支持 geminicli 和 antigravity）
        const expectedType = channelType === 60 ? 'antigravity_oauth_result' : 'geminicli_oauth_result'
        if (event.data && event.data.type === expectedType) {
          // 如果已经处理过，直接返回
          if (oauthHandledRef.current) {
            return
          }

          // 立即设置标志
          oauthHandledRef.current = true

          // 立即停止轮询
          if (pollingIntervalRef.current) {
            clearInterval(pollingIntervalRef.current)
            pollingIntervalRef.current = null
          }

          // 移除自己
          window.removeEventListener('message', handleMessage)
          messageHandlerRef.current = null

          // 处理结果
          if (event.data.success && event.data.credentials) {
            setFieldValue('key', event.data.credentials)
            showSuccess('OAuth 授权成功！凭证已自动填充')
          } else {
            showError('OAuth 授权失败')
          }

          // 更新状态
          setOauthLoading(false)
          setOauthState(null)
          setOauthWindow(null)

          // 关闭弹窗
          if (popup && !popup.closed) {
            popup.close()
          }
        }
      }

      messageHandlerRef.current = handleMessage
      window.addEventListener('message', handleMessage)

      // 开始轮询状态（每 2 秒查询一次）
      const interval = setInterval(async() => {
        const completed = await pollOAuthStatus(state, setFieldValue, messageHandlerRef, channelType)
        if (completed) {
          clearInterval(interval)
          pollingIntervalRef.current = null
        }
      }, 2000)

      pollingIntervalRef.current = interval

      // 检测弹窗是否被关闭
      const checkClosed = setInterval(() => {
        if (popup && popup.closed) {
          clearInterval(checkClosed)
          // 不立即停止轮询，因为用户可能在其他浏览器完成授权
          if (messageHandlerRef.current) {
            window.removeEventListener('message', messageHandlerRef.current)
            messageHandlerRef.current = null
          }
        }
      }, 500)

      // 10 分钟后超时
      setTimeout(() => {
        // 检查是否已经处理过
        if (oauthHandledRef.current) {
          return
        }

        if (pollingIntervalRef.current) {
          clearInterval(pollingIntervalRef.current)
          pollingIntervalRef.current = null
        }
        if (messageHandlerRef.current) {
          window.removeEventListener('message', messageHandlerRef.current)
          messageHandlerRef.current = null
        }
        if (oauthLoading) {
          setOauthLoading(false)
          setOauthState(null)
          showError('OAuth 授权超时，请重试')
        }
      }, 10 * 60 * 1000)

    } catch (error) {
      showError('OAuth 授权失败: ' + (error.message || error))
      setOauthLoading(false)
    }
  }

  // ClaudeCode OAuth 授权处理 - 步骤1: 获取授权链接
  const handleClaudeCodeOAuth = async(proxy) => {
    const trimmedProxy = proxy ? proxy.trim() : ''

    try {
      setClaudeCodeSubmitting(true)

      // 调用后端 API 生成授权 URL（传递代理配置）
      const res = await API.post('/api/claudecode/oauth/start', {
        channel_id: channelId || 0,
        proxy: trimmedProxy
      })

      if (!res.data.success) {
        showError(res.data.message || '获取授权链接失败')
        setClaudeCodeSubmitting(false)
        return
      }

      const authURL = res.data.data.auth_url
      const sessionId = res.data.data.session_id

      setClaudeCodeAuthURL(authURL)
      setClaudeCodeSessionId(sessionId)
      setClaudeCodeOAuthVisible(true)
      setClaudeCodeSubmitting(false)

      // 自动打开授权页面
      window.open(authURL, '_blank')

    } catch (error) {
      showError('获取授权链接失败: ' + (error.message || error))
      setClaudeCodeSubmitting(false)
    }
  }

  // ClaudeCode OAuth 授权处理 - 步骤2: 提交授权码
  const handleClaudeCodeSubmitCode = async(setFieldValue) => {
    if (!claudeCodeAuthCode || claudeCodeAuthCode.trim() === '') {
      showError('请输入授权码或回调 URL')
      return
    }

    try {
      setClaudeCodeSubmitting(true)

      // 提交授权码到后端
      const res = await API.post('/api/claudecode/oauth/exchange-code', {
        session_id: claudeCodeSessionId,
        callback_url: claudeCodeAuthCode.trim()
      })

      if (!res.data.success) {
        showError(res.data.message || '授权码交换失败')
        setClaudeCodeSubmitting(false)
        return
      }

      // 获取凭证并填充
      const credentials = res.data.data.credentials
      setFieldValue('key', credentials)
      showSuccess('OAuth 授权成功！凭证已自动填充')

      // 关闭对话框并重置状态
      setClaudeCodeOAuthVisible(false)
      setClaudeCodeAuthURL('')
      setClaudeCodeSessionId('')
      setClaudeCodeAuthCode('')
      setClaudeCodeSubmitting(false)

    } catch (error) {
      showError('授权码交换失败: ' + (error.message || error))
      setClaudeCodeSubmitting(false)
    }
  }

  // 取消 ClaudeCode OAuth
  const handleClaudeCodeCancelOAuth = () => {
    setClaudeCodeOAuthVisible(false)
    setClaudeCodeAuthURL('')
    setClaudeCodeSessionId('')
    setClaudeCodeAuthCode('')
    setClaudeCodeSubmitting(false)
  }

  // Codex OAuth 授权处理 - 步骤1: 获取授权链接
  const handleCodexOAuth = async(proxy) => {
    const trimmedProxy = proxy ? proxy.trim() : ''

    try {
      setCodexSubmitting(true)

      // 调用后端 API 生成授权 URL（传递代理配置）
      const res = await API.post('/api/codex/oauth/start', {
        channel_id: channelId || 0,
        proxy: trimmedProxy
      })

      if (!res.data.success) {
        showError(res.data.message || '获取授权链接失败')
        setCodexSubmitting(false)
        return
      }

      const authURL = res.data.data.auth_url
      const sessionId = res.data.data.session_id

      setCodexAuthURL(authURL)
      setCodexSessionId(sessionId)
      setCodexOAuthVisible(true)
      setCodexSubmitting(false)

      // 自动打开授权页面
      window.open(authURL, '_blank')

    } catch (error) {
      showError('获取授权链接失败: ' + (error.message || error))
      setCodexSubmitting(false)
    }
  }

  // Codex OAuth 授权处理 - 步骤2: 提交授权码
  const handleCodexSubmitCode = async(setFieldValue) => {
    if (!codexAuthCode || codexAuthCode.trim() === '') {
      showError('请输入授权码或回调 URL')
      return
    }

    try {
      setCodexSubmitting(true)

      // 提交授权码到后端
      const res = await API.post('/api/codex/oauth/exchange-code', {
        session_id: codexSessionId,
        callback_url: codexAuthCode.trim()
      })

      if (!res.data.success) {
        showError(res.data.message || '授权码交换失败')
        setCodexSubmitting(false)
        return
      }

      // 获取凭证并填充
      const credentials = res.data.data.credentials
      setFieldValue('key', credentials)
      showSuccess('OAuth 授权成功！凭证已自动填充')

      // 关闭对话框并重置状态
      setCodexOAuthVisible(false)
      setCodexAuthURL('')
      setCodexSessionId('')
      setCodexAuthCode('')
      setCodexSubmitting(false)

    } catch (error) {
      showError('授权码交换失败: ' + (error.message || error))
      setCodexSubmitting(false)
    }
  }

  // 取消 Codex OAuth
  const handleCodexCancelOAuth = () => {
    setCodexOAuthVisible(false)
    setCodexAuthURL('')
    setCodexSessionId('')
    setCodexAuthCode('')
    setCodexSubmitting(false)
  }

  const handleTypeChange = (setFieldValue, typeValue, values) => {
    // 处理插件事务
    if (pluginList[typeValue]) {
      const newPluginValues = {}
      const pluginConfig = pluginList[typeValue]
      for (const pluginName in pluginConfig) {
        const plugin = pluginConfig[pluginName]
        const oldValve = values['plugin'] ? values['plugin'][pluginName] || {} : {}
        newPluginValues[pluginName] = {}
        for (const paramName in plugin.params) {
          const param = plugin.params[paramName]
          newPluginValues[pluginName][paramName] = oldValve[paramName] || (param.type === 'bool' ? false : '')
        }
      }
      setFieldValue('plugin', newPluginValues)
    }

    const newInput = initChannel(typeValue)

    if (newInput) {
      Object.keys(newInput).forEach((key) => {
        if (
          (!Array.isArray(values[key]) && values[key] !== null && values[key] !== undefined && values[key] !== '') ||
          (Array.isArray(values[key]) && values[key].length > 0)
        ) {
          return
        }

        if (key === 'models') {
          setFieldValue(key, initialModel(newInput[key]))
          return
        }
        setFieldValue(key, newInput[key])
      })
    }
  }

  const basicModels = (channelType) => {
    let modelGroup = typeConfig[channelType]?.modelGroup || defaultConfig.modelGroup
    // 循环 modelOptions，找到 modelGroup 对应的模型
    let modelList = []
    modelOptions.forEach((model) => {
      if (model.group === modelGroup) {
        modelList.push(model)
      }
    })
    return modelList
  }

  const handleModelSelectorConfirm = (selectedModels, overwriteModels) => {
    if (tempSetFieldValue && tempFormikValues) {
      if (overwriteModels) {
        // 覆盖模式：清空现有的模型列表，使用选择器中的模型
        tempSetFieldValue('models', selectedModels)
      } else {
        // 追加模式：合并现有模型和新选择的模型，避免重复
        const existingModels = tempFormikValues.models || []
        const existingModelIds = new Set(existingModels.map((model) => model.id))

        // 过滤掉已存在的模型，避免重复
        const newModels = selectedModels.filter((model) => !existingModelIds.has(model.id))

        // 合并模型列表
        tempSetFieldValue('models', [...existingModels, ...newModels])
      }
    }
  }

  const submit = async(values, { setErrors, setStatus, setSubmitting }) => {
    setSubmitting(true)
    values = trims(values)
    if (values.base_url && values.base_url.endsWith('/')) {
      values.base_url = values.base_url.slice(0, values.base_url.length - 1)
    }
    if (values.type === 3 && values.other === '') {
      values.other = '2024-05-01-preview'
    }
    if (values.type === 18 && values.other === '') {
      values.other = 'v2.1'
    }
    let res

    let modelMappingModel = []

    if (values.model_mapping) {
      try {
        const modelMapping = values.model_mapping.reduce((acc, item) => {
          if (item.key && item.value) {
            acc[item.key] = item.value
          }
          return acc
        }, {})
        const cleanedMapping = {}

        for (const [key, value] of Object.entries(modelMapping)) {
          if (key && value && !(key in cleanedMapping)) {
            cleanedMapping[key] = value
            modelMappingModel.push(key)
          }
        }

        values.model_mapping = JSON.stringify(cleanedMapping, null, 2)
      } catch (error) {
        showError('Error parsing model_mapping:' + error.message)
      }
    }
    let modelHeadersKey = []

    if (values.model_headers) {
      try {
        const modelHeader = values.model_headers.reduce((acc, item) => {
          if (item.key && item.value) {
            acc[item.key] = item.value
          }
          return acc
        }, {})
        const cleanedHeader = {}

        for (const [key, value] of Object.entries(modelHeader)) {
          if (key && value && !(key in cleanedHeader)) {
            cleanedHeader[key] = value
            modelHeadersKey.push(key)
          }
        }

        values.model_headers = JSON.stringify(cleanedHeader, null, 2)
      } catch (error) {
        showError('Error parsing model_headers:' + error.message)
      }
    }

    if (values.custom_parameter) {
      try {
        // Validate that the custom_parameter is valid JSON
        JSON.parse(values.custom_parameter)
      } catch (error) {
        showError('Error parsing custom_parameter: ' + error.message)
        return
      }
    }

    if (values.disabled_stream) {
      values.disabled_stream = removeDuplicates(values.disabled_stream)
    }

    // 获取现有的模型 ID
    const existingModelIds = values.models.map((model) => model.id)

    // 找出在 modelMappingModel 中存在但不在 existingModelIds 中的模型
    const newModelIds = modelMappingModel.filter((id) => !existingModelIds.includes(id))

    // 合并现有的模型 ID 和新的模型 ID，并去重
    const allUniqueModelIds = Array.from(new Set([...existingModelIds, ...newModelIds]))

    // 创建新的 modelsStr
    const modelsStr = allUniqueModelIds.join(',')
    values.group = values.groups.join(',')

    let baseApiUrl = '/api/channel/'

    if (isTag) {
      baseApiUrl = '/api/channel_tag/' + encodeURIComponent(channelId)
    }

    try {
      if (channelId) {
        res = await API.put(baseApiUrl, { ...values, id: parseInt(channelId), models: modelsStr })
      } else {
        res = await API.post(baseApiUrl, { ...values, models: modelsStr })
      }
      const { success, message } = res.data
      if (success) {
        if (channelId) {
          showSuccess(t('channel_edit.editSuccess'))
        } else {
          showSuccess(t('channel_edit.addSuccess'))
        }
        setSubmitting(false)
        setStatus({ success: true })
        onOk(true)

      } else {
        setStatus({ success: false })
        showError(message)
        setErrors({ submit: message })
      }
    } catch (error) {
      setStatus({ success: false })
      showError(error.message)
      setErrors({ submit: error.message })

    }
  }

  function initialModel(channelModel) {
    if (!channelModel) {
      return []
    }

    // 如果 channelModel 是一个字符串
    if (typeof channelModel === 'string') {
      channelModel = channelModel.split(',')
    }
    let modelList = channelModel.map((model) => {
      const modelOption = modelOptions.find((option) => option.id === model)
      if (modelOption) {
        return modelOption
      }
      return { id: model, group: t('channel_edit.customModelTip') }
    })
    return modelList
  }

  const loadChannel = async() => {
    try {
      let baseApiUrl = `/api/channel/${channelId}`

      if (isTag) {
        baseApiUrl = '/api/channel_tag/' + encodeURIComponent(channelId)
      }

      let res = await API.get(baseApiUrl)
      const { success, message, data } = res.data
      if (success) {
        if (data.models === '') {
          data.models = []
        } else {
          data.models = initialModel(data.models)
        }
        if (data.group === '') {
          data.groups = []
        } else {
          data.groups = data.group.split(',')
        }

        data.model_mapping =
          data.model_mapping !== ''
            ? Object.entries(JSON.parse(data.model_mapping)).map(([key, value], index) => ({
              index,
              key,
              value
            }))
            : []

        // 初始化模型原始映射关系
        const mapping = parseModelMapping(data.model_mapping)
        if (mapping) {
          const initialMapping = {}
          // 根据当前的模型映射和模型列表，建立原始映射关系
          Object.entries(mapping).forEach(([key, value]) => {
            const modelExists = data.models.some(model => model.id === key)
            if (modelExists) {
              initialMapping[key] = value
            }
          })
          setModelOriginalMapping(initialMapping)
        } else {
          setModelOriginalMapping({})
        }
        // if (data.model_headers) {
        data.model_headers =
          data.model_headers !== ''
            ? Object.entries(JSON.parse(data.model_headers)).map(([key, value], index) => ({
              index,
              key,
              value
            }))
            : []
        // }

        // Format the custom_parameter JSON for better readability if it's not empty
        if (data.custom_parameter !== '') {
          try {
            // Parse and then stringify with indentation for formatting
            const parsedJson = JSON.parse(data.custom_parameter)
            data.custom_parameter = JSON.stringify(parsedJson, null, 2)
          } catch (error) {
            // If parsing fails, keep the original string
          }
        } else {
          data.custom_parameter = ''
        }

        data.base_url = data.base_url ?? ''
        data.remark = data.remark ?? ''
        data.is_edit = true
        if (data.plugin === null) {
          data.plugin = {}
        }
        initChannel(data.type)
        setInitialInput(data)

        if (!isTag && data.tag) {
          setHasTag(true)
        }
      } else {
        showError(message)
      }
    } catch (error) {

    }
  }

  useEffect(() => {
    if (open) {
      setBatchAdd(isTag)
      if (channelId) {
        loadChannel().then()
      } else {
        setHasTag(false)
        initChannel(1)
        setInitialInput({ ...defaultConfig.input, is_edit: false })
        // 重置模型原始映射关系
        setModelOriginalMapping({})
      }
    } else {
      // 关闭对话框时清理 OAuth 窗口和轮询
      cleanupOAuth()
    }

    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [channelId, open])

  return (
    <Dialog open={open} onClose={handleCancel} fullWidth maxWidth={'md'}>
      <DialogTitle
        sx={{ margin: '0px', fontWeight: 700, lineHeight: '1.55556', padding: '24px', fontSize: '1.125rem' }}>
        {channelId ? t('common.edit') : t('common.create')}
      </DialogTitle>
      <Divider/>
      <DialogContent>
        <Formik initialValues={initialInput} enableReinitialize validationSchema={getValidationSchema(t)}
                onSubmit={submit}>
          {({ errors, handleBlur, handleChange, handleSubmit, isSubmitting, touched, values, setFieldValue }) => {
            // 保存当前Formik状态，以便在模型选择器中使用
            const openModelSelector = () => {
              setTempFormikValues({ ...values })
              setTempSetFieldValue(() => setFieldValue) // 保存函数引用
              setModelSelectorOpen(true)
            }

            return (
              <form noValidate onSubmit={handleSubmit}>
                {!isTag && (
                  <FormControl fullWidth error={Boolean(touched.type && errors.type)}
                               sx={{ ...theme.typography.otherInput }}>
                    <InputLabel htmlFor="channel-type-label">{customizeT(inputLabel.type)}</InputLabel>
                    <Select
                      id="channel-type-label"
                      label={customizeT(inputLabel.type)}
                      value={values.type}
                      name="type"
                      onBlur={handleBlur}
                      onChange={(e) => {
                        handleChange(e)
                        handleTypeChange(setFieldValue, e.target.value, values)
                      }}
                      disabled={hasTag}
                      MenuProps={{
                        PaperProps: {
                          style: {
                            maxHeight: 200
                          }
                        }
                      }}
                    >
                      {Object.values(CHANNEL_OPTIONS).map((option) => {
                        return (
                          <MenuItem key={option.value} value={option.value}>
                            {option.text}
                          </MenuItem>
                        )
                      })}
                    </Select>
                    {touched.type && errors.type ? (
                      <FormHelperText error id="helper-tex-channel-type-label">
                        {errors.type}
                      </FormHelperText>
                    ) : (
                      <FormHelperText
                        id="helper-tex-channel-type-label"> {customizeT(inputPrompt.type)} </FormHelperText>
                    )}
                  </FormControl>
                )}

                <FormControl fullWidth error={Boolean(touched.tag && errors.tag)}
                             sx={{ ...theme.typography.otherInput }}>
                  <InputLabel htmlFor="channel-tag-label">{customizeT(inputLabel.tag)}</InputLabel>
                  <OutlinedInput
                    id="channel-tag-label"
                    label={customizeT(inputLabel.tag)}
                    type="text"
                    value={values.tag}
                    name="tag"
                    onBlur={handleBlur}
                    onChange={handleChange}
                    inputProps={{}}
                    aria-describedby="helper-text-channel-tag-label"
                  />
                  {touched.tag && errors.tag ? (
                    <FormHelperText error id="helper-tex-channel-tag-label">
                      {errors.tag}
                    </FormHelperText>
                  ) : (
                    <FormHelperText id="helper-tex-channel-tag-label"> {customizeT(inputPrompt.tag)} </FormHelperText>
                  )}
                </FormControl>

                {!isTag && (
                  <FormControl fullWidth error={Boolean(touched.name && errors.name)}
                               sx={{ ...theme.typography.otherInput }}>
                    <InputLabel htmlFor="channel-name-label">{customizeT(inputLabel.name)}</InputLabel>
                    <OutlinedInput
                      id="channel-name-label"
                      label={customizeT(inputLabel.name)}
                      type="text"
                      value={values.name}
                      name="name"
                      onBlur={handleBlur}
                      onChange={handleChange}
                      inputProps={{ autoComplete: 'name' }}
                      aria-describedby="helper-text-channel-name-label"
                    />
                    {touched.name && errors.name ? (
                      <FormHelperText error id="helper-tex-channel-name-label">
                        {errors.name}
                      </FormHelperText>
                    ) : (
                      <FormHelperText
                        id="helper-tex-channel-name-label"> {customizeT(inputPrompt.name)} </FormHelperText>
                    )}
                  </FormControl>
                )}
                {inputPrompt.base_url && (
                  <FormControl fullWidth error={Boolean(touched.base_url && errors.base_url)}
                               sx={{ ...theme.typography.otherInput }}>
                    <InputLabel htmlFor="channel-base_url-label">{customizeT(inputLabel.base_url)}</InputLabel>
                    <OutlinedInput
                      id="channel-base_url-label"
                      label={customizeT(inputLabel.base_url)}
                      type="text"
                      value={values.base_url}
                      name="base_url"
                      onBlur={handleBlur}
                      onChange={handleChange}
                      inputProps={{}}
                      aria-describedby="helper-text-channel-base_url-label"
                    />

                    {touched.base_url && errors.base_url ? (
                      <FormHelperText error id="helper-tex-channel-base_url-label">
                        {errors.base_url}
                      </FormHelperText>
                    ) : (
                      <FormHelperText
                        id="helper-tex-channel-base_url-label"> {customizeT(inputPrompt.base_url)} </FormHelperText>
                    )}
                  </FormControl>
                )}

                {inputPrompt.other && (
                  <FormControl fullWidth error={Boolean(touched.other && errors.other)}
                               sx={{ ...theme.typography.otherInput }}>
                    <InputLabel htmlFor="channel-other-label">{customizeT(inputLabel.other)}</InputLabel>
                    <OutlinedInput
                      id="channel-other-label"
                      label={customizeT(inputLabel.other)}
                      type="text"
                      value={values.other}
                      name="other"
                      disabled={hasTag}
                      onBlur={handleBlur}
                      onChange={handleChange}
                      inputProps={{}}
                      aria-describedby="helper-text-channel-other-label"
                    />
                    {touched.other && errors.other ? (
                      <FormHelperText error id="helper-tex-channel-other-label">
                        {errors.other}
                      </FormHelperText>
                    ) : (
                      <FormHelperText
                        id="helper-tex-channel-other-label"> {customizeT(inputPrompt.other)} </FormHelperText>
                    )}
                  </FormControl>
                )}

                <FormControl fullWidth error={Boolean(touched.remark && errors.remark)}
                             sx={{ ...theme.typography.otherInput }}>
                  <InputLabel htmlFor="channel-remark-label">{customizeT(inputLabel.remark)}</InputLabel>
                  <OutlinedInput
                    id="channel-remark-label"
                    label={customizeT(inputLabel.remark)}
                    type="text"
                    value={values.remark}
                    name="remark"
                    onBlur={handleBlur}
                    onChange={handleChange}
                    multiline
                    minRows={2}
                    inputProps={{}}
                    aria-describedby="helper-text-channel-remark-label"
                  />
                  {touched.remark && errors.remark ? (
                    <FormHelperText error id="helper-tex-channel-remark-label">
                      {errors.remark}
                    </FormHelperText>
                  ) : (
                    <FormHelperText
                      id="helper-tex-channel-remark-label"> {customizeT(inputPrompt.remark)} </FormHelperText>
                  )}
                </FormControl>

                <FormControl fullWidth sx={{ ...theme.typography.otherInput }}>
                  <Autocomplete
                    multiple
                    id="channel-groups-label"
                    options={groupOptions}
                    value={values.groups}
                    disabled={hasTag}
                    onChange={(e, value) => {
                      const event = {
                        target: {
                          name: 'groups',
                          value: value
                        }
                      }
                      handleChange(event)
                    }}
                    onBlur={handleBlur}
                    filterSelectedOptions
                    renderInput={(params) => (
                      <TextField {...params} name="groups" error={Boolean(errors.groups)}
                                 label={customizeT(inputLabel.groups)}/>
                    )}
                    aria-describedby="helper-text-channel-groups-label"
                  />
                  {errors.groups ? (
                    <FormHelperText error id="helper-tex-channel-groups-label">
                      {errors.groups}
                    </FormHelperText>
                  ) : (
                    <FormHelperText
                      id="helper-tex-channel-groups-label"> {customizeT(inputPrompt.groups)} </FormHelperText>
                  )}
                </FormControl>

                <FormControl fullWidth sx={{ ...theme.typography.otherInput }}>
                  <Box sx={{ position: 'relative' }}>
                    <Autocomplete
                      multiple
                      freeSolo
                      disableCloseOnSelect
                      id="channel-models-label"
                      disabled={hasTag}
                      options={modelOptions}
                      value={values.models}
                      inputValue={inputValue}
                      onInputChange={(event, newInputValue) => {
                        if (newInputValue.includes(',')) {
                          const modelsList = newInputValue
                            .split(',')
                            .map((item) => ({
                              id: item.trim(),
                              group: t('channel_edit.customModelTip')
                            }))
                            .filter((item) => item.id)

                          const updatedModels = [...new Set([...values.models, ...modelsList])]
                          const event = {
                            target: {
                              name: 'models',
                              value: updatedModels
                            }
                          }
                          handleChange(event)
                          setInputValue('')
                        } else {
                          setInputValue(newInputValue)
                        }
                      }}
                      onChange={(e, value) => {
                        const event = {
                          target: {
                            name: 'models',
                            value: value.map((item) =>
                              typeof item === 'string' ? { id: item, group: t('channel_edit.customModelTip') } : item
                            )
                          }
                        }
                        handleChange(event)
                      }}
                      renderInput={(params) => (
                        <TextField
                          {...params}
                          name="models"
                          error={Boolean(errors.models)}
                          label={customizeT(inputLabel.models)}
                          InputProps={{
                            ...params.InputProps
                          }}
                        />
                      )}
                      groupBy={(option) => option.group}
                      getOptionLabel={(option) => {
                        if (typeof option === 'string') {
                          return option
                        }
                        if (option.inputValue) {
                          return option.inputValue
                        }
                        return option.id
                      }}
                      filterOptions={(options, params) => {
                        const filtered = filter(options, params)
                        const { inputValue } = params
                        const isExisting = options.some((option) => inputValue === option.id)
                        if (inputValue !== '' && !isExisting) {
                          filtered.push({
                            id: inputValue,
                            group: t('channel_edit.customModelTip')
                          })
                        }
                        return filtered
                      }}
                      renderOption={(props, option, { selected }) => (
                        <li {...props}>
                          <Checkbox icon={icon} checkedIcon={checkedIcon} style={{ marginRight: 8 }}
                                    checked={selected}/>
                          {option.id}
                        </li>
                      )}
                      renderTags={(value, getTagProps) =>
                        value.map((option, index) => {
                          const tagProps = getTagProps({ index })
                          return (
                            <Chip
                              key={index}
                              label={option.id}
                              {...tagProps}
                              onClick={() => copy(option.id)}
                              sx={{
                                maxWidth: '100%',
                                height: 'auto',
                                margin: '3px',
                                '& .MuiChip-label': {
                                  whiteSpace: 'normal',
                                  wordBreak: 'break-word',
                                  padding: '6px 8px',
                                  lineHeight: 1.4,
                                  fontWeight: 400
                                },
                                '& .MuiChip-deleteIcon': {
                                  margin: '0 5px 0 -6px'
                                }
                              }}
                            />
                          )
                        })
                      }
                      sx={{
                        '& .MuiAutocomplete-tag': {
                          margin: '2px'
                        },
                        '& .MuiAutocomplete-inputRoot': {
                          flexWrap: 'wrap'
                        }
                      }}
                    />
                  </Box>
                  {errors.models ? (
                    <FormHelperText error id="helper-tex-channel-models-label">
                      {errors.models}
                    </FormHelperText>
                  ) : (
                    <FormHelperText
                      id="helper-tex-channel-models-label"> {customizeT(inputPrompt.models)} </FormHelperText>
                  )}
                </FormControl>
                <Container
                  sx={{
                    textAlign: isMobile ? 'center' : 'right',
                    p: 0
                  }}
                >
                  {!isMobile ? (
                    <ButtonGroup
                      variant="outlined"
                      aria-label="small outlined primary button group"
                    >
                      <Button
                        size="small"
                        onClick={() => {
                          const modelString = values.models.map((model) => model.id).join(',')
                          copy(modelString)
                        }}
                      >
                        {t('channel_edit.copyModels')}
                      </Button>
                      <Button
                        disabled={hasTag}
                        size="small"
                        onClick={() => {
                          setFieldValue('models', basicModels(values.type))
                        }}
                      >
                        {t('channel_edit.inputChannelModel')}
                      </Button>
                      {inputLabel.provider_models_list && (
                        <Tooltip title={customizeT(inputPrompt.provider_models_list)} placement="top">
                          <Button
                            disabled={hasTag}
                            size="small"
                            onClick={openModelSelector}
                            startIcon={<Icon icon="mdi:cloud-download"/>}
                          >
                            {customizeT(inputLabel.provider_models_list)}
                          </Button>
                        </Tooltip>
                      )}
                    </ButtonGroup>
                  ) : (
                    <Stack
                      direction="row"
                      spacing={1}
                      divider={<Divider orientation="vertical" flexItem/>}
                      justifyContent="space-around"
                      alignItems="center"
                    >
                      <IconButton
                        size="small"
                        onClick={() => {
                          const modelString = values.models.map((model) => model.id).join(',')
                          copy(modelString)
                        }}
                      >
                        <Icon icon="mdi:content-copy"/>
                      </IconButton>
                      <IconButton
                        disabled={hasTag}
                        size="small"
                        onClick={() => {
                          setFieldValue('models', basicModels(values.type))
                        }}
                      >
                        <Icon icon="mdi:playlist-plus"/>
                      </IconButton>
                      {inputLabel.provider_models_list && (
                        <Tooltip title={customizeT(inputPrompt.provider_models_list)} placement="top">
                          <IconButton
                            disabled={hasTag}
                            size="small"
                            onClick={openModelSelector}
                          >
                            <Icon icon="mdi:cloud-download"/>
                          </IconButton>
                        </Tooltip>
                      )}
                    </Stack>
                  )}
                </Container>
                <FormControl fullWidth error={Boolean(touched.key && errors.key)}
                             sx={{ ...theme.typography.otherInput }}>
                  {!batchAdd ? (
                    <>
                      <InputLabel htmlFor="channel-key-label">{customizeT(inputLabel.key)}</InputLabel>
                      <OutlinedInput
                        id="channel-key-label"
                        label={customizeT(inputLabel.key)}
                        type="text"
                        value={values.key}
                        name="key"
                        onBlur={handleBlur}
                        onChange={handleChange}
                        inputProps={{}}
                        aria-describedby="helper-text-channel-key-label"
                      />
                    </>
                  ) : (
                    <TextField
                      multiline
                      id="channel-key-label"
                      label={customizeT(inputLabel.key)}
                      value={values.key}
                      name="key"
                      onBlur={handleBlur}
                      onChange={handleChange}
                      aria-describedby="helper-text-channel-key-label"
                      minRows={5}
                      maxRows={15}
                      placeholder={customizeT(inputPrompt.key) + t('channel_edit.batchKeytip')}
                    />
                  )}

                  {touched.key && errors.key ? (
                    <FormHelperText error id="helper-tex-channel-key-label">
                      {errors.key}
                    </FormHelperText>
                  ) : (
                    <FormHelperText id="helper-tex-channel-key-label">
                      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                        <span>{customizeT(inputPrompt.key)}</span>
                        {channelId === 0 && (
                          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <Switch
                              size="small"
                              checked={Boolean(batchAdd)}
                              onChange={(e) => setBatchAdd(e.target.checked)}
                            />
                            <Typography variant="body2">{t('channel_edit.batchAdd')}</Typography>
                          </Box>
                        )}
                      </Box>
                    </FormHelperText>
                  )}
                </FormControl>

                {/* GeminiCli/Antigravity OAuth 授权按钮 */}
                {(values.type === 57 || values.type === 60) && !batchAdd && (
                  <Box sx={{ mt: 2, mb: 2 }}>
                    <Box sx={{ display: 'flex', gap: 1 }}>
                      <Button
                        variant="outlined"
                        color="primary"
                        fullWidth
                        disabled={oauthLoading}
                        onClick={() => handleGeminiCliOAuth(values.other, values.proxy, setFieldValue, values.type)}
                        startIcon={oauthLoading ? null : <Icon icon="mdi:google"/>}
                      >
                        {oauthLoading ? '授权中，请完成授权...' : 'OAuth 授权'}
                      </Button>
                      <Button
                        variant="outlined"
                        color="secondary"
                        disabled={!oauthURL}
                        onClick={handleCopyOAuthURL}
                        startIcon={<Icon icon="mdi:content-copy"/>}
                        sx={{ minWidth: '120px' }}
                      >
                        复制链接
                      </Button>
                    </Box>
                    <Alert severity="info" sx={{ mt: 1 }}>
                      {values.other ? (
                        <>授权后会跳转到 localhost:8080，请在浏览器地址栏中将 localhost:8080 改为当前服务的域名后刷新访问完成授权</>
                      ) : (
                        <>
                          <strong>Project ID 未填写，将自动检测可用项目。</strong>
                          <br />
                          授权后会跳转到 localhost:8080，请在浏览器地址栏中将 localhost:8080 改为当前服务的域名后刷新访问完成授权
                        </>
                      )}
                    </Alert>
                  </Box>
                )}

                {/* ClaudeCode OAuth 授权按钮 */}
                {values.type === 58 && !batchAdd && (
                  <Box sx={{ mt: 2, mb: 2 }}>
                    <Button
                      variant="outlined"
                      color="primary"
                      fullWidth
                      disabled={claudeCodeSubmitting}
                      onClick={() => handleClaudeCodeOAuth(values.proxy)}
                      startIcon={claudeCodeSubmitting ? null : <Icon icon="simple-icons:anthropic"/>}
                    >
                      {claudeCodeSubmitting ? '获取授权链接中...' : 'OAuth 授权'}
                    </Button>
                    <Alert severity="info" sx={{ mt: 1 }}>
                      点击按钮后，将打开 Claude 授权页面。授权成功后，请复制浏览器地址栏中的完整 URL 并粘贴到弹出的输入框中。
                    </Alert>

                    {/* ClaudeCode OAuth 对话框 */}
                    <Dialog
                      open={claudeCodeOAuthVisible}
                      onClose={handleClaudeCodeCancelOAuth}
                      maxWidth="md"
                      fullWidth
                    >
                      <DialogTitle>ClaudeCode OAuth 授权</DialogTitle>
                      <DialogContent>
                        <Box sx={{ mb: 2 }}>
                          <Alert severity="info" sx={{ mb: 2 }}>
                            <Typography variant="body2" component="div">
                              <strong>授权步骤：</strong>
                              <ol style={{ margin: '8px 0', paddingLeft: '20px' }}>
                                <li>点击下方"打开授权页面"按钮（或手动复制链接到浏览器）</li>
                                <li>在新打开的页面中登录 Claude 账户并同意授权</li>
                                <li>授权成功后，复制浏览器地址栏中的<strong>完整 URL</strong></li>
                                <li>将完整 URL 粘贴到下方输入框中，点击"提交授权码"</li>
                              </ol>
                            </Typography>
                          </Alert>

                          <Box sx={{ mb: 2, display: 'flex', gap: 1 }}>
                            <Button
                              variant="contained"
                              color="primary"
                              fullWidth
                              onClick={() => window.open(claudeCodeAuthURL, '_blank')}
                              startIcon={<Icon icon="mdi:open-in-new"/>}
                            >
                              打开授权页面
                            </Button>
                            <Button
                              variant="outlined"
                              color="secondary"
                              onClick={() => {
                                copy(claudeCodeAuthURL).then(() => {
                                  showSuccess('授权链接已复制到剪贴板')
                                }).catch(() => {
                                  showError('复制失败，请手动复制')
                                })
                              }}
                              startIcon={<Icon icon="mdi:content-copy"/>}
                              sx={{ minWidth: '120px' }}
                            >
                              复制链接
                            </Button>
                          </Box>

                          <TextField
                            fullWidth
                            label="授权回调 URL 或授权码"
                            placeholder="粘贴完整的回调 URL，例如：https://console.anthropic.com/oauth/code/callback?code=xxx&state=xxx"
                            value={claudeCodeAuthCode}
                            onChange={(e) => setClaudeCodeAuthCode(e.target.value)}
                            multiline
                            rows={3}
                            variant="outlined"
                          />
                        </Box>
                      </DialogContent>
                      <DialogActions>
                        <Button onClick={handleClaudeCodeCancelOAuth} disabled={claudeCodeSubmitting}>
                          取消
                        </Button>
                        <Button
                          onClick={() => handleClaudeCodeSubmitCode(setFieldValue)}
                          variant="contained"
                          color="primary"
                          disabled={claudeCodeSubmitting || !claudeCodeAuthCode}
                        >
                          {claudeCodeSubmitting ? '提交中...' : '提交授权码'}
                        </Button>
                      </DialogActions>
                    </Dialog>
                  </Box>
                )}

                {/* Codex OAuth 授权按钮 */}
                {values.type === 59 && !batchAdd && (
                  <Box sx={{ mt: 2, mb: 2 }}>
                    <Button
                      variant="outlined"
                      color="primary"
                      fullWidth
                      disabled={codexSubmitting}
                      onClick={() => handleCodexOAuth(values.proxy)}
                      startIcon={codexSubmitting ? null : <Icon icon="simple-icons:openai"/>}
                    >
                      {codexSubmitting ? '获取授权链接中...' : 'OAuth 授权'}
                    </Button>
                    <Alert severity="info" sx={{ mt: 1 }}>
                      点击按钮后，将打开 OpenAI 授权页面。授权成功后，请复制浏览器地址栏中的完整 URL 并粘贴到弹出的输入框中。
                    </Alert>

                    {/* Codex OAuth 对话框 */}
                    <Dialog
                      open={codexOAuthVisible}
                      onClose={handleCodexCancelOAuth}
                      maxWidth="md"
                      fullWidth
                    >
                      <DialogTitle>Codex OAuth 授权</DialogTitle>
                      <DialogContent>
                        <Box sx={{ mb: 2 }}>
                          <Alert severity="info" sx={{ mb: 2 }}>
                            <Typography variant="body2" component="div">
                              <strong>授权步骤：</strong>
                              <ol style={{ margin: '8px 0', paddingLeft: '20px' }}>
                                <li>点击下方"打开授权页面"按钮（或手动复制链接到浏览器）</li>
                                <li>在新打开的页面中登录 OpenAI 账户并同意授权</li>
                                <li>授权成功后，复制浏览器地址栏中的<strong>完整 URL</strong></li>
                                <li>将完整 URL 粘贴到下方输入框中，点击"提交授权码"</li>
                              </ol>
                            </Typography>
                          </Alert>

                          <Box sx={{ mb: 2, display: 'flex', gap: 1 }}>
                            <Button
                              variant="contained"
                              color="primary"
                              fullWidth
                              onClick={() => window.open(codexAuthURL, '_blank')}
                              startIcon={<Icon icon="mdi:open-in-new"/>}
                            >
                              打开授权页面
                            </Button>
                            <Button
                              variant="outlined"
                              color="secondary"
                              onClick={() => {
                                copy(codexAuthURL).then(() => {
                                  showSuccess('授权链接已复制到剪贴板')
                                }).catch(() => {
                                  showError('复制失败，请手动复制')
                                })
                              }}
                              startIcon={<Icon icon="mdi:content-copy"/>}
                              sx={{ minWidth: '120px' }}
                            >
                              复制链接
                            </Button>
                          </Box>

                          <TextField
                            fullWidth
                            label="授权回调 URL 或授权码"
                            placeholder="粘贴完整的回调 URL，例如：http://localhost:1455/auth/callback?code=xxx&state=xxx"
                            value={codexAuthCode}
                            onChange={(e) => setCodexAuthCode(e.target.value)}
                            multiline
                            rows={3}
                            variant="outlined"
                          />
                        </Box>
                      </DialogContent>
                      <DialogActions>
                        <Button onClick={handleCodexCancelOAuth} disabled={codexSubmitting}>
                          取消
                        </Button>
                        <Button
                          onClick={() => handleCodexSubmitCode(setFieldValue)}
                          variant="contained"
                          color="primary"
                          disabled={codexSubmitting || !codexAuthCode}
                        >
                          {codexSubmitting ? '提交中...' : '提交授权码'}
                        </Button>
                      </DialogActions>
                    </Dialog>
                  </Box>
                )}

                {inputPrompt.model_mapping && (
                  <FormControl
                    fullWidth
                    error={Boolean(touched.model_mapping && errors.model_mapping)}
                    sx={{ ...theme.typography.otherInput }}
                  >
                    <MapInput
                      mapValue={values.model_mapping}
                      onChange={(newValue) => {
                        setFieldValue('model_mapping', newValue)
                        // 实时同步模型重定向到模型配置
                        syncModelMappingToModels(newValue, values.models, setFieldValue)
                      }}
                      disabled={hasTag}
                      error={Boolean(touched.model_mapping && errors.model_mapping)}
                      label={{
                        keyName: customizeT(inputLabel.model_mapping),
                        valueName: customizeT(inputPrompt.model_mapping),
                        name: customizeT(inputLabel.model_mapping)
                      }}
                    />
                    {touched.model_mapping && errors.model_mapping ? (
                      <FormHelperText error id="helper-tex-channel-model_mapping-label">
                        {errors.model_mapping}
                      </FormHelperText>
                    ) : (
                      <FormHelperText
                        id="helper-tex-channel-model_mapping-label">{customizeT(inputPrompt.model_mapping)}</FormHelperText>
                    )}
                  </FormControl>
                )}
                {inputPrompt.model_headers && (
                  <FormControl
                    fullWidth
                    error={Boolean(touched.model_headers && errors.model_headers)}
                    sx={{ ...theme.typography.otherInput }}
                  >
                    <MapInput
                      mapValue={values.model_headers}
                      onChange={(newValue) => {
                        setFieldValue('model_headers', newValue)
                      }}
                      disabled={hasTag}
                      error={Boolean(touched.model_headers && errors.model_headers)}
                      label={{
                        keyName: customizeT(inputLabel.model_headers),
                        valueName: customizeT(inputPrompt.model_headers),
                        name: customizeT(inputLabel.model_headers)
                      }}
                    />
                    {touched.model_headers && errors.model_headers ? (
                      <FormHelperText error id="helper-tex-channel-model_headers-label">
                        {errors.model_headers}
                      </FormHelperText>
                    ) : (
                      <FormHelperText
                        id="helper-tex-channel-model_headers-label">{customizeT(inputPrompt.model_headers)}</FormHelperText>
                    )}
                  </FormControl>
                )}
                {inputPrompt.custom_parameter && (
                  <FormControl
                    fullWidth
                    error={Boolean(touched.custom_parameter && errors.custom_parameter)}
                    sx={{ ...theme.typography.otherInput }}
                  >
                    <InputLabel shrink htmlFor="channel-custom_parameter-label">
                      {customizeT(inputLabel.custom_parameter)}
                    </InputLabel>
                    <Box
                      sx={{
                        border: '1px solid',
                        borderColor: touched.custom_parameter && errors.custom_parameter ? 'error.main' : 'divider',
                        borderRadius: 1,
                        overflow: 'hidden',
                        marginTop: 2, // Add some margin for the label
                        resize: 'vertical',
                        height: '200px',
                        minHeight: '100px',
                        '&:hover': {
                          borderColor: 'primary.main'
                        },
                        '&:focus-within': {
                          borderColor: 'primary.main',
                          borderWidth: 2
                        }
                      }}
                    >
                      <Editor
                        height="100%"
                        language="json"
                        theme={theme.palette.mode === 'dark' ? 'vs-dark' : 'light'}
                        value={values.custom_parameter}
                        options={{
                          minimap: { enabled: false },
                          scrollBeyondLastLine: false,
                          automaticLayout: true,
                          fontSize: 14,
                          lineNumbers: 'on',
                          folding: true,
                          formatOnPaste: true,
                          formatOnType: true
                        }}
                        onChange={(value) => {
                          setFieldValue('custom_parameter', value);
                        }}
                      />
                    </Box>
                    {touched.custom_parameter && errors.custom_parameter ? (
                      <FormHelperText error id="helper-tex-channel-custom_parameter-label">
                        {errors.custom_parameter}
                      </FormHelperText>
                    ) : (
                      <FormHelperText id="helper-tex-channel-custom_parameter-label">
                        {customizeT(inputPrompt.custom_parameter)}
                      </FormHelperText>
                    )}
                  </FormControl>
                )}
                {inputPrompt.disabled_stream && (
                  <FormControl
                    fullWidth
                    error={Boolean(touched.disabled_stream && errors.disabled_stream)}
                    sx={{ ...theme.typography.otherInput }}
                  >
                    <ListInput
                      listValue={values.disabled_stream}
                      onChange={(newValue) => {
                        setFieldValue('disabled_stream', newValue)
                      }}
                      disabled={hasTag}
                      error={Boolean(touched.disabled_stream && errors.disabled_stream)}
                      label={{
                        name: customizeT(inputLabel.disabled_stream),
                        itemName: customizeT(inputPrompt.disabled_stream)
                      }}
                    />
                  </FormControl>
                )}

                <FormControl fullWidth error={Boolean(touched.proxy && errors.proxy)}
                             sx={{ ...theme.typography.otherInput }}>
                  <InputLabel htmlFor="channel-proxy-label">{customizeT(inputLabel.proxy)}</InputLabel>
                  <OutlinedInput
                    id="channel-proxy-label"
                    label={customizeT(inputLabel.proxy)}
                    disabled={hasTag}
                    type="text"
                    value={values.proxy}
                    name="proxy"
                    onBlur={handleBlur}
                    onChange={handleChange}
                    inputProps={{}}
                    aria-describedby="helper-text-channel-proxy-label"
                  />
                  {touched.proxy && errors.proxy ? (
                    <FormHelperText error id="helper-tex-channel-proxy-label">
                      {errors.proxy}
                    </FormHelperText>
                  ) : (
                    <FormHelperText
                      id="helper-tex-channel-proxy-label"> {customizeT(inputPrompt.proxy)} </FormHelperText>
                  )}
                </FormControl>
                {inputPrompt.test_model && (
                  <FormControl fullWidth error={Boolean(touched.test_model && errors.test_model)}
                               sx={{ ...theme.typography.otherInput }}>
                    <InputLabel htmlFor="channel-test_model-label">{customizeT(inputLabel.test_model)}</InputLabel>
                    <OutlinedInput
                      id="channel-test_model-label"
                      label={customizeT(inputLabel.test_model)}
                      type="text"
                      disabled={hasTag}
                      value={values.test_model}
                      name="test_model"
                      onBlur={handleBlur}
                      onChange={handleChange}
                      inputProps={{}}
                      aria-describedby="helper-text-channel-test_model-label"
                    />
                    {touched.test_model && errors.test_model ? (
                      <FormHelperText error id="helper-tex-channel-test_model-label">
                        {errors.test_model}
                      </FormHelperText>
                    ) : (
                      <FormHelperText
                        id="helper-tex-channel-test_model-label"> {customizeT(inputPrompt.test_model)} </FormHelperText>
                    )}
                  </FormControl>
                )}
                {inputPrompt.only_chat && (
                  <FormControl fullWidth>
                    <FormControlLabel
                      control={
                        <Switch
                          disabled={hasTag}
                          checked={Boolean(values.only_chat)}
                          onChange={(event) => {
                            setFieldValue('only_chat', event.target.checked)
                          }}
                        />
                      }
                      label={customizeT(inputLabel.only_chat)}
                    />
                    <FormHelperText
                      id="helper-tex-only_chat_model-label"> {customizeT(inputPrompt.only_chat)} </FormHelperText>
                  </FormControl>
                )}

                {inputPrompt.pre_cost && (
                  <FormControl fullWidth error={Boolean(touched.pre_cost && errors.pre_cost)}
                               sx={{ ...theme.typography.otherInput }}>
                    <InputLabel htmlFor="channel-pre_cost-label">{customizeT(inputLabel.pre_cost)}</InputLabel>
                    <Select
                      id="channel-pre_cost-label"
                      label={customizeT(inputLabel.pre_cost)}
                      value={values.pre_cost}
                      name="pre_cost"
                      onBlur={handleBlur}
                      onChange={handleChange}
                      disabled={hasTag}
                      MenuProps={{
                        PaperProps: {
                          style: {
                            maxHeight: 200
                          }
                        }
                      }}
                    >
                      {PreCostType.map((option) => {
                        return (
                          <MenuItem key={option.value} value={option.value}>
                            {option.label}
                          </MenuItem>
                        )
                      })}
                    </Select>
                    {touched.pre_cost && errors.pre_cost ? (
                      <FormHelperText error id="helper-tex-channel-pre_cost-label">
                        {errors.pre_cost}
                      </FormHelperText>
                    ) : (
                      <FormHelperText
                        id="helper-tex-channel-pre_cost-label"> {customizeT(inputPrompt.pre_cost)} </FormHelperText>
                    )}
                  </FormControl>
                )}
                {inputPrompt.compatible_response && (
                  <FormControl fullWidth>
                    <FormControlLabel
                      control={
                        <Switch
                          disabled={hasTag}
                          checked={Boolean(values.compatible_response)}
                          onChange={(event) => {
                            setFieldValue('compatible_response', event.target.checked)
                          }}
                        />
                      }
                      label={customizeT(inputLabel.compatible_response)}
                    />
                    <FormHelperText
                      id="helper-tex-compatible_response-label">{customizeT(inputPrompt.compatible_response)}</FormHelperText>
                  </FormControl>
                )}
                {pluginList[values.type] &&
                  Object.keys(pluginList[values.type]).map((pluginId) => {
                    const plugin = pluginList[values.type][pluginId]
                    return (
                      <>
                        <Box
                          sx={{
                            border: '1px solid #e0e0e0',
                            borderRadius: 2,
                            marginTop: 2,
                            marginBottom: 2,
                            overflow: 'hidden'
                          }}
                        >
                          <Box
                            sx={{
                              display: 'flex',
                              justifyContent: 'space-between',
                              alignItems: 'center',
                              padding: 2
                            }}
                          >
                            <Box sx={{ flex: 1 }}>
                              <Typography variant="h3">{customizeT(plugin.name)}</Typography>
                              <Typography variant="caption">{customizeT(plugin.description)}</Typography>
                            </Box>
                            <Button
                              onClick={() => setExpanded(!expanded)}
                              endIcon={
                                expanded ? (
                                  <Icon icon="solar:alt-arrow-up-line-duotone"/>
                                ) : (
                                  <Icon icon="solar:alt-arrow-down-line-duotone"/>
                                )
                              }
                              sx={{ textTransform: 'none', marginLeft: 2 }}
                            >
                              {expanded ? t('channel_edit.collapse') : t('channel_edit.expand')}
                            </Button>
                          </Box>

                          <Collapse in={expanded}>
                            <Box sx={{ padding: 2, marginTop: -3 }}>
                              {Object.keys(plugin.params).map((paramId) => {
                                const param = plugin.params[paramId]
                                const name = `plugin.${pluginId}.${paramId}`
                                return param.type === 'bool' ? (
                                  <FormControl key={name} fullWidth sx={{ ...theme.typography.otherInput }}>
                                    <FormControlLabel
                                      key={name}
                                      required
                                      control={
                                        <Switch
                                          key={name}
                                          name={name}
                                          disabled={hasTag}
                                          checked={values.plugin?.[pluginId]?.[paramId] || false}
                                          onChange={(event) => {
                                            setFieldValue(name, event.target.checked)
                                          }}
                                        />
                                      }
                                      label={t('channel_edit.isEnable')}
                                    />
                                    <FormHelperText
                                      id="helper-tex-channel-key-label"> {customizeT(param.description)} </FormHelperText>
                                  </FormControl>
                                ) : (
                                  <FormControl key={name} fullWidth sx={{ ...theme.typography.otherInput }}>
                                    <TextField
                                      multiline
                                      key={name}
                                      name={name}
                                      disabled={hasTag}
                                      value={values.plugin?.[pluginId]?.[paramId] || ''}
                                      label={customizeT(param.name)}
                                      placeholder={customizeT(param.description)}
                                      onChange={handleChange}
                                    />
                                    <FormHelperText
                                      id="helper-tex-channel-key-label"> {customizeT(param.description)} </FormHelperText>
                                  </FormControl>
                                )
                              })}
                            </Box>
                          </Collapse>
                        </Box>
                      </>
                    )
                  })}
                <DialogActions>
                  <Button onClick={onCancel}>{t('common.cancel')}</Button>
                  <Button disableElevation disabled={isSubmitting} type="submit" variant="contained" color="primary">
                    {t('common.submit')}
                  </Button>
                </DialogActions>
              </form>
            )
          }}
        </Formik>

        {/* 模型选择器弹窗 */}
        <ModelSelectorModal
          open={modelSelectorOpen}
          onClose={() => setModelSelectorOpen(false)}
          onConfirm={(selectedModels, mappings, overwriteModels, overwriteMappings) => {
            // 处理普通模型选择
            handleModelSelectorConfirm(selectedModels, overwriteModels)

            // 处理映射关系
            if (mappings && mappings.length > 0) {
              if (overwriteMappings) {
                // 覆盖映射模式：清空现有映射，使用新的
                tempSetFieldValue('model_mapping', mappings)
              } else {
                // 追加映射模式：
                const existingMappings = tempFormikValues?.model_mapping || []
                const existingKeys = new Set(existingMappings.map((item) => item.key))
                const newMappings = mappings.filter((item) => !existingKeys.has(item.key))
                const mergedMappings = [...existingMappings, ...newMappings].map((item, index) => ({
                  ...item,
                  index
                }))
                tempSetFieldValue('model_mapping', mergedMappings)
              }
            }
          }}
          channelValues={tempFormikValues}
          prices={prices}
        />
      </DialogContent>
    </Dialog>
  )
}

export default EditModal

EditModal.propTypes = {
  open: PropTypes.bool,
  channelId: PropTypes.oneOfType([PropTypes.number, PropTypes.string]),
  onCancel: PropTypes.func,
  onOk: PropTypes.func,
  groupOptions: PropTypes.array,
  isTag: PropTypes.bool,
  modelOptions: PropTypes.array,
  prices: PropTypes.array
}
