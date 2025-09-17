package controller

import (
	"done-hub/common"
	"done-hub/model"
	paymentService "done-hub/payment"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// GetPaymentList godoc
// @Summary List payments (admin)
// @Description 获取支付网关列表（管理员）
// @Tags Payment
// @Produce json
// @Param page query int false "页码"
// @Param size query int false "每页数量"
// @Param order query string false "排序"
// @Success 200 {object} map[string]interface{}
// @Router /payment/ [get]
func GetPaymentList(c *gin.Context) {
	var params model.SearchPaymentParams
	if err := c.ShouldBindQuery(&params); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	payments, err := model.GetPanymentList(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    payments,
	})
}

// GetPayment godoc
// @Summary Get payment (admin)
// @Description 获取支付网关详情（管理员）
// @Tags Payment
// @Produce json
// @Param id path int true "网关ID"
// @Success 200 {object} map[string]interface{}
// @Router /payment/{id} [get]
func GetPayment(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	payment, err := model.GetPaymentByID(id)
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
		"data":    payment,
	})
}

// AddPayment godoc
// @Summary Create payment (admin)
// @Description 新增支付网关（管理员）
// @Tags Payment
// @Accept json
// @Produce json
// @Param body body model.Payment true "网关信息"
// @Success 201 {object} map[string]interface{}
// @Router /payment/ [post]
func AddPayment(c *gin.Context) {
	payment := model.Payment{}
	err := c.ShouldBindJSON(&payment)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if err := payment.Insert(); err != nil {
		common.APIRespondWithError(c, http.StatusInternalServerError, err)
		return
	}

	ps, err := paymentService.NewPaymentService(payment.UUID)
	if err != nil {
		if deleteErr := payment.Delete(); deleteErr != nil {
			log.Printf("Failed to delete payment after service creation error: %v", deleteErr)
		}
		common.APIRespondWithError(c, http.StatusInternalServerError, err)
		return
	}

	if err := ps.CreatedPay(); err != nil {
		if deleteErr := payment.Delete(); deleteErr != nil {
			log.Printf("Failed to delete payment after creation error: %v", deleteErr)
		}
		common.APIRespondWithError(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "Payment added successfully",
		"data":    payment,
	})
}

// UpdatePayment godoc
// @Summary Update payment (admin)
// @Description 更新支付网关（管理员）
// @Tags Payment
// @Accept json
// @Produce json
// @Param body body model.Payment true "网关信息"
// @Success 200 {object} map[string]interface{}
// @Router /payment/ [put]
func UpdatePayment(c *gin.Context) {
	payment := model.Payment{}
	err := c.ShouldBindJSON(&payment)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	overwrite := true

	if payment.UUID == "" {
		overwrite = false
	}

	err = payment.Update(overwrite)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    payment,
	})
}

// DeletePayment godoc
// @Summary Delete payment (admin)
// @Description 删除支付网关（管理员）
// @Tags Payment
// @Produce json
// @Param id path int true "网关ID"
// @Success 200 {object} map[string]interface{}
// @Router /payment/{id} [delete]
func DeletePayment(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	payment := model.Payment{ID: id}
	err = payment.Delete()
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

// GetUserPaymentList godoc
// @Summary List enabled payments (user)
// @Description 获取可用支付网关（用户）
// @Tags Payment
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /payment/user [get]
func GetUserPaymentList(c *gin.Context) {
	payments, err := model.GetUserPaymentList()
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    payments,
	})
}
