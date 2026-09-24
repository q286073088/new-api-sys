package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func performTopupDetailsRequest(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, path, nil)
	switch method {
	case http.MethodGet:
		GetTopUpDetails(ctx)
	}
	return recorder
}

func TestTopUpDetailsListAndSummary(t *testing.T) {
	db := setupManageUserTestDB(t)
	user := model.User{Username: "order-owner", DisplayName: "Order Owner", Role: common.RoleCommonUser, AffCode: "order-aff"}
	require.NoError(t, db.Create(&user).Error)
	gift := model.TopUp{UserId: user.Id, Amount: 300, TradeNo: "ADMIN_GIFT", PaymentMethod: model.PaymentMethodAdmin, PaymentProvider: model.PaymentProviderAdmin, Status: common.TopUpStatusSuccess, IsGift: true, CompleteTime: 100}
	require.NoError(t, db.Create(&gift).Error)
	unbilledAdmin := model.TopUp{UserId: user.Id, Amount: 400, TradeNo: "ADMIN_UNBILLED", PaymentMethod: model.PaymentMethodAdmin, Status: common.TopUpStatusSuccess, CompleteTime: 150}
	require.NoError(t, db.Create(&unbilledAdmin).Error)
	invoiceCents := int64(6600)
	billedAdmin := model.TopUp{UserId: user.Id, Amount: 500, Money: 66, InvoiceAmountCents: &invoiceCents, TradeNo: "ADMIN_BILLED", PaymentMethod: model.PaymentMethodAdmin, PaymentProvider: model.PaymentProviderAdmin, Status: common.TopUpStatusSuccess, CompleteTime: 250}
	require.NoError(t, db.Create(&billedAdmin).Error)
	paid := model.TopUp{UserId: user.Id, Amount: 10, Money: 88, TradeNo: "USR1NOEPAY", PaymentMethod: "alipay", PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusSuccess, CompleteTime: 200}
	require.NoError(t, db.Create(&paid).Error)
	onlineGift := model.TopUp{UserId: user.Id, Amount: 6, Money: 12, TradeNo: "ONLINE_GIFT", PaymentMethod: model.PaymentMethodStripe, PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess, IsGift: true, CompleteTime: 220}
	require.NoError(t, db.Create(&onlineGift).Error)
	pending := model.TopUp{UserId: user.Id, Amount: 5, Money: 44, TradeNo: "USR2NOEPAY", PaymentMethod: "wxpay", PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusPending}
	require.NoError(t, db.Create(&pending).Error)

	recorder := performTopupDetailsRequest(t, http.MethodGet, "/api/user/topup/details?keyword=USR1")
	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.True(t, gjson.Get(body, "success").Bool())
	assert.Equal(t, int64(1), gjson.Get(body, "data.total").Int())
	assert.Equal(t, "order-owner", gjson.Get(body, "data.items.0.username").String())
	assert.NotContains(t, body, "password")

	recorder = performTopupDetailsRequest(t, http.MethodGet, "/api/user/topup/details?keyword=ADMIN")
	require.Equal(t, http.StatusOK, recorder.Code)
	adminBody := recorder.Body.String()
	assert.Contains(t, adminBody, "ADMIN_BILLED")
	assert.NotContains(t, adminBody, "ADMIN_UNBILLED")
	assert.NotContains(t, adminBody, "ADMIN_GIFT")

	recorder = performTopupDetailsRequest(t, http.MethodGet, "/api/user/topup/details?keyword=ONLINE_GIFT")
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, int64(1), gjson.Get(recorder.Body.String(), "data.total").Int())
	assert.Contains(t, recorder.Body.String(), "ONLINE_GIFT")

	recorder = performTopupDetailsRequest(t, http.MethodGet, "/api/user/topup/details?status=pending")
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, int64(1), gjson.Get(recorder.Body.String(), "data.total").Int())

	recorder = httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/topup/details/summary/"+itoa(user.Id), nil)
	ctx.Params = gin.Params{{Key: "userId", Value: itoa(user.Id)}}
	GetTopUpDetailSummary(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	summary := gjson.Get(recorder.Body.String(), "data")
	assert.Equal(t, int64(4), summary.Get("order_count").Int())
	assert.Equal(t, int64(3), summary.Get("success_count").Int())
	assert.Equal(t, 154.0, summary.Get("total_paid_money").Float())
	assert.Equal(t, int64(1), summary.Get("gift_count").Int())
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
