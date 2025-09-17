package router

import (
	"done-hub/controller"
	"done-hub/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func SetApiRouter(engine *gin.Engine) {
	api := engine.Group("/api")
	api.Use(gzip.Gzip(gzip.DefaultCompression))
	api.Use(middleware.CORS())
	api.Use(middleware.SecurityHeaders())
	api.Use(middleware.NoCache())

	// metrics
	api.GET("/metrics", middleware.MetricsWithBasicAuth(), gin.WrapH(promhttp.Handler()))

	// 全局限流
	api.Use(middleware.GlobalAPIRateLimit())

	// 健康检查/基础信息
	api.GET("/status", controller.GetStatus)

	// 邮件相关
	api.GET("/verification", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), controller.SendEmailVerification)
	api.GET("/reset_password", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), controller.SendPasswordResetEmail)
	api.POST("/user/reset", middleware.CriticalRateLimit(), controller.ResetPassword)

	// OIDC 登录
	api.GET("/oauth/endpoint", middleware.CriticalRateLimit(), middleware.SessionSecurity(), controller.OIDCEndpoint)
	api.GET("/oauth/oidc", middleware.CriticalRateLimit(), middleware.SessionSecurity(), controller.OIDCAuth)
	api.GET("/oauth/email/bind", middleware.CriticalRateLimit(), middleware.SessionSecurity(), middleware.UserAuth(), controller.EmailBind)

	// 用户体系
	user := api.Group("/user")
	{
		user.POST("/register", controller.Register)
		user.POST("/login", controller.Login)
		user.GET("/logout", controller.Logout)

		self := user.Group("/")
		self.Use(middleware.UserAuth())
		self.GET("/self", controller.GetSelf)
		self.PUT("/self", controller.UpdateSelf)
		self.GET("/token", controller.GenerateAccessToken)

		admin := user.Group("/")
		admin.Use(middleware.AdminAuth())
		admin.GET("/", controller.GetUsersList)
		admin.GET("/:id", controller.GetUser)
		admin.POST("/", controller.CreateUser)
		admin.PUT("/", controller.UpdateUser)
		admin.DELETE("/:id", controller.DeleteUser)
	}

	// 选项配置（精简版配置中心）
	option := api.Group("/option")
	option.Use(middleware.RootAuth())
	option.GET("/", controller.GetOptions)
	option.PUT("/", controller.UpdateOption)

	// 支付回调
	api.Any("/payment/notify/:uuid", controller.PaymentCallback)

	// 支付网关（管理员）
	payment := api.Group("/payment")
	payment.Use(middleware.AdminAuth())
	payment.GET("/", controller.GetPaymentList)
	payment.GET("/:id", controller.GetPayment)
	payment.POST("/", controller.AddPayment)
	payment.PUT("/", controller.UpdatePayment)
	payment.DELETE("/:id", controller.DeletePayment)

	// 订单（用户发起与查询；管理员查询列表）
	order := api.Group("/order")
	order.Use(middleware.UserAuth())
	order.POST("/", controller.CreateOrder)
	order.GET("/status", controller.CheckOrderStatus)
	orderAdmin := api.Group("/order")
	orderAdmin.Use(middleware.AdminAuth())
	orderAdmin.GET("/", controller.GetOrderList)
}
