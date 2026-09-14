package operation_setting

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"golang.org/x/net/idna"
)

const EpayDomainConfigsKey = "EpayDomainConfigs"

type EpayMerchant struct {
	MerchantID string `json:"merchant_id"`
	Key        string `json:"key,omitempty"`
	PayAddress string `json:"pay_address"`
}

type EpayDomainConfig struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
	EpayMerchant
	KeyConfigured bool `json:"key_configured,omitempty"`
}

var epayDomainConfigs struct {
	sync.RWMutex
	items []EpayDomainConfig
}

// NormalizeEpayDomain matches site hostnames, independent of scheme and port.
// Callers handling requests must pass Request.Host, never client-supplied
// Origin, Referer or forwarding headers.
func NormalizeEpayDomain(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 1024 {
		return "", errors.New("域名不能为空或过长")
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip.String(), nil
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", errors.New("请输入有效域名，不包含路径、查询参数或通配符")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("域名端口无效")
		}
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if ip := net.ParseIP(host); ip != nil {
		return ip.String(), nil
	}
	host, err = idna.Lookup.ToASCII(host)
	if err != nil || host == "" || len(host) > 253 {
		return "", errors.New("域名无效")
	}
	for label := range strings.SplitSeq(host, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", errors.New("域名无效")
		}
		for _, ch := range label {
			if ch != '-' && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
				return "", errors.New("域名无效")
			}
		}
	}
	return host, nil
}

func ValidateEpayAddress(address string) error {
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || len(address) > 2048 {
		return errors.New("易支付地址必须是有效的 HTTP(S) 地址，不能包含账号、查询参数或片段")
	}
	return nil
}

// ParseEpayDomainConfigs preserves a masked secret only for the same saved
// merchant and gateway. Changing a hostname does not rotate that credential.
func ParseEpayDomainConfigs(raw string, previous []EpayDomainConfig) ([]EpayDomainConfig, error) {
	if len(raw) > 128*1024 {
		return nil, errors.New("易支付域名配置过大")
	}
	var items []EpayDomainConfig
	if err := common.UnmarshalJsonStr(raw, &items); err != nil {
		return nil, errors.New("易支付域名配置必须是 JSON 数组")
	}
	if len(items) > 100 {
		return nil, errors.New("最多配置 100 个易支付域名")
	}
	known := make(map[string]EpayDomainConfig, len(previous))
	for _, item := range previous {
		known[item.ID] = item
	}
	domains, ids := make(map[string]bool), make(map[string]bool)
	for i := range items {
		item := &items[i]
		domain, err := NormalizeEpayDomain(item.Domain)
		if err != nil {
			return nil, fmt.Errorf("第 %d 条配置：%w", i+1, err)
		}
		if domains[domain] {
			return nil, fmt.Errorf("域名 %s 重复配置", domain)
		}
		domains[domain] = true
		item.Domain = domain
		if item.ID == "" {
			item.ID = common.GetUUID()
		}
		if len(item.ID) > 64 || ids[item.ID] {
			return nil, errors.New("易支付配置标识无效或重复")
		}
		ids[item.ID] = true
		item.MerchantID = strings.TrimSpace(item.MerchantID)
		item.PayAddress = strings.TrimRight(strings.TrimSpace(item.PayAddress), "/")
		item.Key = strings.TrimSpace(item.Key)
		if item.MerchantID == "" || len(item.MerchantID) > 64 || strings.ContainsAny(item.MerchantID, "\r\n\t ") {
			return nil, fmt.Errorf("域名 %s 的商户 ID 无效", domain)
		}
		if item.PayAddress != "" {
			if err := ValidateEpayAddress(item.PayAddress); err != nil {
				return nil, fmt.Errorf("域名 %s：%w", domain, err)
			}
		}
		if item.Key == "" {
			old, ok := known[item.ID]
			if ok && old.MerchantID == item.MerchantID && old.PayAddress == item.PayAddress {
				item.Key = old.Key
			}
		}
		if item.Key == "" || len(item.Key) > 512 || strings.ContainsAny(item.Key, "\r\n") {
			return nil, fmt.Errorf("域名 %s 需要有效的商户密钥；更换商户 ID 或支付网关后须重新填写", domain)
		}
		item.KeyConfigured = false
	}
	if items == nil {
		items = []EpayDomainConfig{}
	}
	return items, nil
}

func UpdateEpayDomainConfigs(raw string) error {
	items, err := ParseEpayDomainConfigs(raw, nil)
	if err != nil {
		return err
	}
	epayDomainConfigs.Lock()
	epayDomainConfigs.items = items
	epayDomainConfigs.Unlock()
	return nil
}

func GetEpayDomainConfigs() []EpayDomainConfig {
	epayDomainConfigs.RLock()
	defer epayDomainConfigs.RUnlock()
	return append([]EpayDomainConfig{}, epayDomainConfigs.items...)
}

func GetEpayMerchant(host string) EpayMerchant {
	domain, _ := NormalizeEpayDomain(host)
	items := GetEpayDomainConfigs()
	common.OptionMapRWMutex.RLock()
	merchant := EpayMerchant{MerchantID: EpayId, Key: EpayKey, PayAddress: PayAddress}
	common.OptionMapRWMutex.RUnlock()
	for _, item := range items {
		if domain == item.Domain {
			address := item.PayAddress
			if address == "" {
				address = merchant.PayAddress
			}
			return EpayMerchant{MerchantID: item.MerchantID, Key: item.Key, PayAddress: address}
		}
	}
	return merchant
}

type PaymentSetting struct {
	AmountOptions  []int           `json:"amount_options"`
	AmountDiscount map[int]float64 `json:"amount_discount"` // 充值金额对应的折扣，例如 100 元 0.9 表示 100 元充值享受 9 折优惠

	ComplianceConfirmed    bool   `json:"compliance_confirmed"`
	ComplianceTermsVersion string `json:"compliance_terms_version"`
	ComplianceConfirmedAt  int64  `json:"compliance_confirmed_at"`
	ComplianceConfirmedBy  int    `json:"compliance_confirmed_by"`
	ComplianceConfirmedIP  string `json:"compliance_confirmed_ip"`
}

const CurrentComplianceTermsVersion = "v1"

// 默认配置
var paymentSetting = PaymentSetting{
	AmountOptions:  []int{10, 20, 50, 100, 200, 500},
	AmountDiscount: map[int]float64{},
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("payment_setting", &paymentSetting)
}

func GetPaymentSetting() *PaymentSetting {
	return &paymentSetting
}

func IsPaymentComplianceConfirmed() bool {
	return paymentSetting.ComplianceConfirmed &&
		paymentSetting.ComplianceTermsVersion == CurrentComplianceTermsVersion
}
