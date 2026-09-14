package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/webhook"
	"gorm.io/gorm"
)

func confirmPaymentComplianceForTest(t *testing.T) {
	t.Helper()
	paymentSetting := operation_setting.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
}

func TestStripeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPISecret := setting.StripeApiSecret
	originalWebhookSecret := setting.StripeWebhookSecret
	originalPriceID := setting.StripePriceId
	t.Cleanup(func() {
		setting.StripeApiSecret = originalAPISecret
		setting.StripeWebhookSecret = originalWebhookSecret
		setting.StripePriceId = originalPriceID
	})

	setting.StripeWebhookSecret = ""
	setting.StripeApiSecret = "sk_test_123"
	setting.StripePriceId = "price_123"
	require.False(t, isStripeWebhookEnabled())

	setting.StripeWebhookSecret = "whsec_test"
	require.True(t, isStripeWebhookEnabled())

	setting.StripePriceId = ""
	require.False(t, isStripeWebhookEnabled())
}

func TestStripeWebhookRetriesFailedReferralTransactionWithoutDuplicateCredit(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.ReferralReward{}, &model.SubscriptionOrder{}))
	originalSecret, originalWebhook, originalPrice := setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId
	originalUnit, originalReferral := common.QuotaPerUnit, setting.GetReferralSetting()
	t.Cleanup(func() {
		setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId = originalSecret, originalWebhook, originalPrice
		common.QuotaPerUnit = originalUnit
		require.NoError(t, setting.UpdateReferralSetting(common.GetJsonString(originalReferral)))
	})
	setting.StripeApiSecret, setting.StripeWebhookSecret, setting.StripePriceId = "sk_test_referral", "whsec_referral_test", "price_referral"
	common.QuotaPerUnit = 1000
	require.NoError(t, setting.UpdateReferralSetting(`{"enabled":true,"level1_percent":5,"level2_percent":2,"delay_days":3}`))
	for _, user := range []model.User{
		{Id: 1, Username: "grandparent", AffCode: "grandparent"},
		{Id: 2, Username: "parent", AffCode: "parent", InviterId: 1},
		{Id: 3, Username: "buyer", AffCode: "buyer", InviterId: 2},
	} {
		require.NoError(t, db.Create(&user).Error)
	}
	order := model.TopUp{UserId: 3, Amount: 100, Money: 100, TradeNo: "stripe-referral-retry",
		PaymentProvider: model.PaymentProviderStripe, PaymentMethod: model.PaymentMethodStripe, Status: common.TopUpStatusPending}
	require.NoError(t, order.Insert())
	require.NoError(t, db.Migrator().DropTable(&model.ReferralReward{}))
	for i, eventType := range []stripe.EventType{
		stripe.EventTypeCheckoutSessionCompleted,
		stripe.EventTypeCheckoutSessionCompleted,
		stripe.EventTypeCheckoutSessionCompleted,
		stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded,
	} {
		if i == 1 {
			require.NoError(t, db.AutoMigrate(&model.ReferralReward{}))
		}
		payload, err := common.Marshal(map[string]any{
			"object": "event", "type": eventType,
			"data": map[string]any{"object": map[string]any{
				"client_reference_id": order.TradeNo, "customer": "cus_referral",
				"status": "complete", "payment_status": "paid", "currency": "usd",
				"amount_total": 9000, "amount_subtotal": 10000,
			}},
		})
		require.NoError(t, err)
		signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{Payload: payload, Secret: setting.StripeWebhookSecret})
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/stripe/webhook", bytes.NewReader(payload))
		ctx.Request.Header.Set("Stripe-Signature", signed.Header)
		StripeWebhook(ctx)
		var buyer model.User
		require.NoError(t, db.First(&buyer, 3).Error)
		require.NoError(t, db.First(&order, order.Id).Error)
		if i == 0 {
			assert.Equal(t, http.StatusInternalServerError, recorder.Code, "Stripe must retry when the rebate transaction rolls back")
			assert.Zero(t, buyer.Quota)
			assert.Equal(t, common.TopUpStatusPending, order.Status)
			continue
		}
		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, 100000, buyer.Quota)
		assert.Equal(t, common.TopUpStatusSuccess, order.Status)
		var rewards []model.ReferralReward
		require.NoError(t, db.Order("level").Find(&rewards).Error)
		require.Len(t, rewards, 2)
		assert.Equal(t, 4500, rewards[0].Quota)
		assert.Equal(t, 1800, rewards[1].Quota)
		var logs int64
		require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeTopup).Count(&logs).Error)
		assert.EqualValues(t, 1, logs)
	}
}

func TestCreemWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPIKey := setting.CreemApiKey
	originalProducts := setting.CreemProducts
	originalWebhookSecret := setting.CreemWebhookSecret
	t.Cleanup(func() {
		setting.CreemApiKey = originalAPIKey
		setting.CreemProducts = originalProducts
		setting.CreemWebhookSecret = originalWebhookSecret
	})

	setting.CreemWebhookSecret = ""
	setting.CreemApiKey = "creem_api_key"
	setting.CreemProducts = `[{"productId":"prod_123"}]`
	require.False(t, isCreemWebhookEnabled())

	setting.CreemWebhookSecret = "creem_secret"
	require.True(t, isCreemWebhookEnabled())

	setting.CreemProducts = "[]"
	require.False(t, isCreemWebhookEnabled())
}

func TestWaffoWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalEnabled := setting.WaffoEnabled
	originalSandbox := setting.WaffoSandbox
	originalAPIKey := setting.WaffoApiKey
	originalPrivateKey := setting.WaffoPrivateKey
	originalPublicCert := setting.WaffoPublicCert
	originalSandboxAPIKey := setting.WaffoSandboxApiKey
	originalSandboxPrivateKey := setting.WaffoSandboxPrivateKey
	originalSandboxPublicCert := setting.WaffoSandboxPublicCert
	t.Cleanup(func() {
		setting.WaffoEnabled = originalEnabled
		setting.WaffoSandbox = originalSandbox
		setting.WaffoApiKey = originalAPIKey
		setting.WaffoPrivateKey = originalPrivateKey
		setting.WaffoPublicCert = originalPublicCert
		setting.WaffoSandboxApiKey = originalSandboxAPIKey
		setting.WaffoSandboxPrivateKey = originalSandboxPrivateKey
		setting.WaffoSandboxPublicCert = originalSandboxPublicCert
	})

	setting.WaffoEnabled = true
	setting.WaffoSandbox = false
	setting.WaffoApiKey = ""
	setting.WaffoPrivateKey = "private"
	setting.WaffoPublicCert = "public"
	require.False(t, isWaffoWebhookEnabled())

	setting.WaffoApiKey = "api"
	require.True(t, isWaffoWebhookEnabled())

	setting.WaffoEnabled = false
	require.False(t, isWaffoWebhookEnabled())

	setting.WaffoEnabled = true
	setting.WaffoSandbox = true
	setting.WaffoSandboxApiKey = ""
	setting.WaffoSandboxPrivateKey = "sandbox_private"
	setting.WaffoSandboxPublicCert = "sandbox_public"
	require.False(t, isWaffoWebhookEnabled())

	setting.WaffoSandboxApiKey = "sandbox_api"
	require.True(t, isWaffoWebhookEnabled())
}

func TestWaffoPancakeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalMerchantID := setting.WaffoPancakeMerchantID
	originalPrivateKey := setting.WaffoPancakePrivateKey
	originalProductID := setting.WaffoPancakeProductID
	t.Cleanup(func() {
		setting.WaffoPancakeMerchantID = originalMerchantID
		setting.WaffoPancakePrivateKey = originalPrivateKey
		setting.WaffoPancakeProductID = originalProductID
	})

	// Presence of all three credentials enables the gateway. Webhook public
	// keys are bundled in the SDK and there is no separate Enabled toggle —
	// clear any of the three fields to disable.
	setting.WaffoPancakeMerchantID = ""
	setting.WaffoPancakePrivateKey = "private"
	setting.WaffoPancakeProductID = "product"
	require.False(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeMerchantID = "merchant"
	require.True(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeProductID = ""
	require.False(t, isWaffoPancakeWebhookEnabled())

	setting.WaffoPancakeProductID = "product"
	setting.WaffoPancakePrivateKey = ""
	require.False(t, isWaffoPancakeWebhookEnabled())
}

func setupEpayDomainPayments(t *testing.T) *gorm.DB {
	t.Helper()
	confirmPaymentComplianceForTest(t)
	db := setupManageUserTestDB(t)
	// Initialize dialect-specific SQL identifiers through the startup path,
	// then keep the separately configured log database from the fixture.
	logDB, master := model.LOG_DB, common.IsMasterNode
	t.Setenv("LOG_SQL_DSN", "")
	common.IsMasterNode = false
	err := model.InitLogDB()
	common.IsMasterNode, model.LOG_DB = master, logDB
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.EpayOrderBinding{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}))
	oldDomains := common.GetJsonString(operation_setting.GetEpayDomainConfigs())
	oldMap, oldUnit := common.OptionMap, common.QuotaPerUnit
	oldID, oldKey, oldAddress := operation_setting.EpayId, operation_setting.EpayKey, operation_setting.PayAddress
	oldPrice, oldMethods := operation_setting.Price, operation_setting.PayMethods
	oldCallback, oldServer := operation_setting.CustomCallbackAddress, system_setting.ServerAddress
	oldReferral := setting.GetReferralSetting()
	t.Cleanup(func() {
		require.NoError(t, operation_setting.UpdateEpayDomainConfigs(oldDomains))
		common.OptionMap, common.QuotaPerUnit = oldMap, oldUnit
		operation_setting.EpayId, operation_setting.EpayKey, operation_setting.PayAddress = oldID, oldKey, oldAddress
		operation_setting.Price, operation_setting.PayMethods = oldPrice, oldMethods
		operation_setting.CustomCallbackAddress, system_setting.ServerAddress = oldCallback, oldServer
		require.NoError(t, setting.UpdateReferralSetting(common.GetJsonString(oldReferral)))
	})
	common.OptionMap, common.QuotaPerUnit = map[string]string{}, 1000
	operation_setting.EpayId, operation_setting.EpayKey, operation_setting.PayAddress = "10000", "default-merchant-secret", "https://pay.default.example"
	operation_setting.Price, operation_setting.PayMethods = 1, []map[string]string{{"type": "alipay"}}
	operation_setting.CustomCallbackAddress, system_setting.ServerAddress = "https://callback.example", "https://console.example"
	require.NoError(t, setting.UpdateReferralSetting(`{"enabled":true,"level1_percent":5,"level2_percent":2,"delay_days":3}`))
	for _, user := range []model.User{
		{Id: 1, Username: "epay-grandparent", AffCode: "epay-grandparent", Email: "grandparent@example.test", Group: "default"},
		{Id: 2, Username: "epay-parent", AffCode: "epay-parent", InviterId: 1, Email: "parent@example.test", Group: "default"},
		{Id: 3, Username: "epay-buyer", AffCode: "epay-buyer", InviterId: 2, Email: "buyer@example.test", Group: "default"},
	} {
		require.NoError(t, db.Create(&user).Error)
	}
	require.NoError(t, model.UpdateOption(operation_setting.EpayDomainConfigsKey, `[
		{"id":"cn","domain":"https://CN.example.com:443/","merchant_id":"10001","key":"cn-merchant-secret","pay_address":"https://pay.cn.example/"},
		{"id":"overseas","domain":"overseas.example.com","merchant_id":"10002","key":"overseas-merchant-secret","pay_address":""}
	]`))
	return db
}

func performEpayRequest(t *testing.T, request *http.Request, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request
	ctx.Set("id", 3)
	handler(ctx)
	return recorder
}

func createDomainEpayCheckout(t *testing.T, host string, subscription bool) map[string]string {
	t.Helper()
	body, handler := `{"amount":10,"payment_method":"alipay"}`, gin.HandlerFunc(RequestEpay)
	if subscription {
		body, handler = `{"plan_id":41,"payment_method":"alipay"}`, SubscriptionRequestEpay
	}
	request := httptest.NewRequest(http.MethodPost, "https://"+host+"/pay", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://overseas.example.com")
	request.Header.Set("X-Forwarded-Host", "overseas.example.com")
	recorder := performEpayRequest(t, request, handler)
	var result struct {
		Message string
		Data    map[string]string
		URL     string
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
	require.Equal(t, "success", result.Message, recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "merchant-secret")
	assert.Contains(t, result.Data["notify_url"], "https://callback.example/")
	if strings.HasPrefix(strings.ToLower(host), "cn.example.com") {
		assert.Equal(t, "https://pay.cn.example/submit.php", result.URL)
	} else {
		assert.Equal(t, "https://pay.default.example/submit.php", result.URL)
	}
	return result.Data
}

func epayCallbackRequest(method, tradeNo, merchantID, key, money string) *http.Request {
	params := epay.GenerateParams(map[string]string{
		"pid": merchantID, "type": "alipay", "out_trade_no": tradeNo,
		"trade_no": "provider-" + tradeNo, "money": money, "trade_status": epay.StatusTradeSuccess,
	}, key)
	values := make(url.Values, len(params))
	for key, value := range params {
		values.Set(key, value)
	}
	if method == http.MethodPost {
		request := httptest.NewRequest(method, "https://callback.example/notify", strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return request
	}
	return httptest.NewRequest(method, "https://different-callback.example/notify?"+values.Encode(), nil)
}

func TestEpayDomainCheckoutSelectsMerchantFromHost(t *testing.T) {
	setupEpayDomainPayments(t)
	for _, tc := range []struct{ host, merchant, domain string }{
		{"CN.example.com:8443", "10001", "cn.example.com"},
		{"overseas.example.com", "10002", "overseas.example.com"},
		{"unmatched.example.com", "10000", "unmatched.example.com"},
		{"child.cn.example.com", "10000", "child.cn.example.com"},
		{"127.0.0.1:3180", "10000", "127.0.0.1"},
		{"[::1]:3180", "10000", "::1"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			params := createDomainEpayCheckout(t, tc.host, false)
			assert.Equal(t, tc.merchant, params["pid"], "Origin and forwarding headers must not select a merchant")
			binding, err := model.GetEpayOrderBinding(params["out_trade_no"])
			require.NoError(t, err)
			assert.Equal(t, tc.merchant, binding.MerchantID)
			assert.Equal(t, tc.domain, binding.Domain)
			assert.JSONEq(t, `{}`, common.GetJsonString(binding), "credentials must never be serialized")
		})
	}
	operation_setting.EpayId, operation_setting.EpayKey = "", ""
	for _, tc := range []struct {
		host    string
		enabled bool
	}{{"cn.example.com", true}, {"unmatched.example.com", false}} {
		request := httptest.NewRequest(http.MethodGet, "https://"+tc.host+"/api/user/topup/info", nil)
		recorder := performEpayRequest(t, request, GetTopUpInfo)
		var response struct {
			Data struct {
				Enabled bool `json:"enable_online_topup"`
			}
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		assert.Equal(t, tc.enabled, response.Data.Enabled)
		assert.NotContains(t, recorder.Body.String(), "merchant-secret")
	}
}

func TestEpayDomainCallbackKeepsOriginalMerchantAndCreditsOnce(t *testing.T) {
	db := setupEpayDomainPayments(t)
	params := createDomainEpayCheckout(t, "cn.example.com", false)
	tradeNo := params["out_trade_no"]
	// A changed/removed mapping and a different callback hostname must not
	// affect an order that was already sent to its original merchant.
	require.NoError(t, model.UpdateOption(operation_setting.EpayDomainConfigsKey, `[]`))
	operation_setting.EpayId, operation_setting.EpayKey = "20000", "rotated-default-secret"
	for _, tc := range []struct{ name, merchant, key, money string }{
		{"other_merchant", "10002", "overseas-merchant-secret", "10.00"},
		{"wrong_key", "10001", "overseas-merchant-secret", "10.00"},
		{"wrong_amount", "10001", "cn-merchant-secret", "0.01"},
		{"exponent_amount", "10001", "cn-merchant-secret", "1e1000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := performEpayRequest(t, epayCallbackRequest(http.MethodPost, tradeNo, tc.merchant, tc.key, tc.money), EpayNotify)
			assert.Equal(t, "fail", recorder.Body.String())
			assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(tradeNo).Status)
		})
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		recorder := performEpayRequest(t, epayCallbackRequest(method, tradeNo, "10001", "cn-merchant-secret", "10.00"), EpayNotify)
		assert.Equal(t, "success", recorder.Body.String())
	}
	var buyer model.User
	require.NoError(t, db.First(&buyer, 3).Error)
	assert.Equal(t, 10000, buyer.Quota)
	var rewards []model.ReferralReward
	require.NoError(t, db.Order("level").Find(&rewards).Error)
	require.Len(t, rewards, 2)
	assert.Equal(t, 500, rewards[0].Quota)
	assert.Equal(t, 200, rewards[1].Quota)
	var mailCount int64
	require.NoError(t, db.Model(&model.EmailNotification{}).Count(&mailCount).Error)
	assert.EqualValues(t, 1, mailCount)
	legacy := &model.TopUp{UserId: 3, Amount: 2, Money: 2, TradeNo: "legacy-default-order", PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusPending}
	require.NoError(t, legacy.Insert())
	recorder := performEpayRequest(t, epayCallbackRequest(http.MethodGet, legacy.TradeNo, "20000", "rotated-default-secret", "2.00"), EpayNotify)
	assert.Equal(t, "success", recorder.Body.String(), "pre-upgrade orders keep the default gateway path")
}

func TestEpayDomainSubscriptionNotifyAndReturnUseOrderMerchant(t *testing.T) {
	db := setupEpayDomainPayments(t)
	plan := model.SubscriptionPlan{Id: 41, Title: "Domain Plan", PriceAmount: 10, Currency: "USD", DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, Enabled: true, TotalAmount: 1000}
	require.NoError(t, db.Create(&plan).Error)
	params := createDomainEpayCheckout(t, "overseas.example.com", true)
	tradeNo := params["out_trade_no"]
	require.NoError(t, model.UpdateOption(operation_setting.EpayDomainConfigsKey, `[]`))
	recorder := performEpayRequest(t, epayCallbackRequest(http.MethodPost, tradeNo, "10001", "cn-merchant-secret", "10.00"), SubscriptionEpayNotify)
	assert.Equal(t, "fail", recorder.Body.String())
	recorder = performEpayRequest(t, epayCallbackRequest(http.MethodGet, tradeNo, "10002", "overseas-merchant-secret", "0.01"), SubscriptionEpayReturn)
	assert.Contains(t, recorder.Header().Get("Location"), "pay=fail")
	recorder = performEpayRequest(t, epayCallbackRequest(http.MethodGet, tradeNo, "10002", "overseas-merchant-secret", "10.00"), SubscriptionEpayReturn)
	assert.Contains(t, recorder.Header().Get("Location"), "pay=success")
	recorder = performEpayRequest(t, epayCallbackRequest(http.MethodPost, tradeNo, "10002", "overseas-merchant-secret", "10.00"), SubscriptionEpayNotify)
	assert.Equal(t, "success", recorder.Body.String())
	var subscriptions int64
	require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ?", 3).Count(&subscriptions).Error)
	assert.EqualValues(t, 1, subscriptions)
	_, err := service.VerifyEpayPayment(params, model.EpayOrderTopUp)
	assert.Error(t, err, "a subscription receipt must not fund a wallet")
}

func TestEpayDomainCallbackMatchesRoundedCheckoutAmount(t *testing.T) {
	db := setupEpayDomainPayments(t)
	operation_setting.Price = 0.1005
	params := createDomainEpayCheckout(t, "cn.example.com", false)
	require.Equal(t, "1.00", params["money"])
	recorder := performEpayRequest(t, epayCallbackRequest(http.MethodPost, params["out_trade_no"], "10001", "cn-merchant-secret", params["money"]), EpayNotify)
	assert.Equal(t, "success", recorder.Body.String(), "verify the amount sent to Epay, including its rounding")
	var buyer model.User
	require.NoError(t, db.First(&buyer, 3).Error)
	assert.Equal(t, 10000, buyer.Quota)
}

func TestEpayDomainCallbackRejectsUnavailableBindingStorage(t *testing.T) {
	db := setupEpayDomainPayments(t)
	params := createDomainEpayCheckout(t, "unmatched.example.com", false)
	require.NoError(t, db.Migrator().DropTable(&model.EpayOrderBinding{}))
	recorder := performEpayRequest(t, epayCallbackRequest(http.MethodPost, params["out_trade_no"], "10000", "default-merchant-secret", "10.00"), EpayNotify)
	assert.Equal(t, "fail", recorder.Body.String(), "database errors must not fall back to default credentials")
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(params["out_trade_no"]).Status)
}

func TestEpayDomainSettingsMaskAndPreserveSecrets(t *testing.T) {
	db := setupEpayDomainPayments(t)
	recorder := performEpayRequest(t, httptest.NewRequest(http.MethodGet, "/api/option/", nil), GetOptions)
	assert.NotContains(t, recorder.Body.String(), "merchant-secret")
	var response struct{ Data []model.Option }
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	var masked []operation_setting.EpayDomainConfig
	for _, option := range response.Data {
		if option.Key == operation_setting.EpayDomainConfigsKey {
			require.NoError(t, common.UnmarshalJsonStr(option.Value, &masked))
		}
	}
	require.Len(t, masked, 2)
	assert.Empty(t, masked[0].Key)
	assert.True(t, masked[0].KeyConfigured)
	masked[0].Domain = "renamed.example.com"
	body := common.GetJsonString(OptionUpdateRequest{Key: operation_setting.EpayDomainConfigsKey, Value: common.GetJsonString(masked)})
	recorder = performEpayRequest(t, httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(body)), UpdateOption)
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.Equal(t, "cn-merchant-secret", operation_setting.GetEpayMerchant("renamed.example.com").Key)
	var saved model.Option
	require.NoError(t, db.Where(&model.Option{Key: operation_setting.EpayDomainConfigsKey}).First(&saved).Error)
	for _, raw := range []string{
		`{}`,
		`[{"domain":"a.example/path","merchant_id":"1","key":"new-key"}]`,
		`[{"domain":"a.example","merchant_id":"1","key":"new-key","pay_address":"javascript:alert(1)"}]`,
		`[{"domain":"a.example","merchant_id":"1","key":"new-key"},{"domain":"https://A.EXAMPLE:443/","merchant_id":"2","key":"new-key"}]`,
		`[{"id":"cn","domain":"renamed.example.com","merchant_id":"different-merchant","key_configured":true}]`,
	} {
		body = common.GetJsonString(OptionUpdateRequest{Key: operation_setting.EpayDomainConfigsKey, Value: raw})
		recorder = performEpayRequest(t, httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(body)), UpdateOption)
		assert.Contains(t, recorder.Body.String(), `"success":false`, fmt.Sprintf("invalid config %q", raw))
		var unchanged model.Option
		require.NoError(t, db.Where(&model.Option{Key: operation_setting.EpayDomainConfigsKey}).First(&unchanged).Error)
		assert.Equal(t, saved.Value, unchanged.Value)
	}
}

func TestEpayDomainSettingsRequireRootAndKeepAuditFreeOfSecrets(t *testing.T) {
	db := setupEpayDomainPayments(t)
	router := gin.New()
	router.GET("/api/option/", middleware.RootAuth(), GetOptions)
	router.PUT("/api/option/", middleware.RootAuth(), UpdateOption)
	const config = `[{"id":"root-account","domain":"root.example.com","merchant_id":"10003","key":"root-merchant-secret"}]`
	body := common.GetJsonString(OptionUpdateRequest{Key: operation_setting.EpayDomainConfigsKey, Value: config})
	for i, tc := range []struct {
		name   string
		role   int
		status int
	}{
		{"anonymous", 0, http.StatusUnauthorized},
		{"user", common.RoleCommonUser, http.StatusForbidden},
		{"admin", common.RoleAdminUser, http.StatusForbidden},
		{"root", common.RoleRootUser, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := "epay-settings-pat-" + tc.name
			if tc.role > 0 {
				user := model.User{Id: 100 + i, Username: tc.name, AffCode: tc.name, Role: tc.role, Status: common.UserStatusEnabled, AuthVersion: 1, AccessToken: &token}
				require.NoError(t, db.Create(&user).Error)
			}
			for _, method := range []string{http.MethodGet, http.MethodPut} {
				request := httptest.NewRequest(method, "/api/option/", strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				if tc.role > 0 {
					request.Header.Set("Authorization", "Bearer "+token)
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				assert.Equal(t, tc.status, recorder.Code, recorder.Body.String())
				assert.NotContains(t, recorder.Body.String(), "merchant-secret")
			}
			if tc.role != common.RoleRootUser {
				assert.Equal(t, "10001", operation_setting.GetEpayMerchant("cn.example.com").MerchantID)
				return
			}
			assert.Equal(t, "10003", operation_setting.GetEpayMerchant("root.example.com").MerchantID)
		})
	}
	var audits []model.AuditLog
	require.NoError(t, model.LOG_DB.Find(&audits).Error)
	require.NotEmpty(t, audits)
	encoded := common.GetJsonString(audits)
	assert.Contains(t, encoded, "option.update")
	assert.NotContains(t, encoded, "merchant-secret")
	assert.NotContains(t, encoded, "epay-settings-pat-")
}

func TestEpayTopUpEnabledRequiresMerchantAndPaymentMethods(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
	})

	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "epay_id"
	operation_setting.EpayKey = ""
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}}
	require.False(t, isEpayTopUpEnabled(""))

	operation_setting.EpayKey = "epay_key"
	require.True(t, isEpayTopUpEnabled(""))

	operation_setting.PayMethods = nil
	require.False(t, isEpayTopUpEnabled(""))
}
