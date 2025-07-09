package geoip

import (
	"net"
	"strings"
	"time"
	"unicode"

	"github.com/komari-monitor/komari/database/config"
	"github.com/komari-monitor/komari/utils"
	"github.com/patrickmn/go-cache"
)

var CurrentProvider GeoIPService
var geoCache *cache.Cache

type GeoInfo struct {
	ISOCode string
	Name    string
}

func init() {
	CurrentProvider = &EmptyProvider{}
	geoCache = cache.New(48*time.Hour, 1*time.Hour)
	utils.LogDebug("GeoIP: Initialized with EmptyProvider")
}

// GeoIPService 接口定义了获取地理位置信息的核心方法。
// 任何实现此接口的类型都可以作为地理位置服务提供者。
type GeoIPService interface {
	Name() string

	GetGeoInfo(ip net.IP) (*GeoInfo, error)

	UpdateDatabase() error

	Close() error
}

func GetRegionUnicodeEmoji(isoCode string) string {
	utils.LogDebug("GeoIP: Converting ISO code '%s' to emoji", isoCode)

	if len(isoCode) != 2 {
		utils.LogDebug("GeoIP: Invalid ISO code length: %d", len(isoCode))
		return ""
	}
	isoCode = strings.ToUpper(isoCode)

	if !unicode.IsLetter(rune(isoCode[0])) || !unicode.IsLetter(rune(isoCode[1])) {
		utils.LogDebug("GeoIP: ISO code contains non-letter characters: %s", isoCode)
		return ""
	}

	rune1 := rune(0x1F1E6 + (rune(isoCode[0]) - 'A'))
	rune2 := rune(0x1F1E6 + (rune(isoCode[1]) - 'A'))
	emoji := string(rune1) + string(rune2)
	utils.LogDebug("GeoIP: Generated emoji '%s' for ISO code '%s'", emoji, isoCode)
	return emoji
}

func InitGeoIp() {
	utils.LogDebug("GeoIP: Starting initialization...")

	conf, err := config.Get()
	if err != nil {
		utils.LogDebug("GeoIP: Failed to get configuration: %v", err)
		panic("Failed to get configuration for GeoIP: " + err.Error())
	}

	utils.LogDebug("GeoIP: Configuration loaded - Enabled: %v, Provider: %s", conf.GeoIpEnabled, conf.GeoIpProvider)

	if !conf.GeoIpEnabled {
		utils.LogDebug("GeoIP: GeoIP is disabled in configuration")
		return
	}

	switch conf.GeoIpProvider {
	case "mmdb":
		utils.LogDebug("GeoIP: Initializing MaxMind provider...")
		NewCurrentProvider, err := NewMaxMindGeoIPService()
		if err != nil {
			utils.LogDebug("GeoIP: MaxMind initialization failed: %v", err)
			utils.LogError("Failed to initialize MaxMind GeoIP service: " + err.Error())
		}
		if NewCurrentProvider != nil {
			CurrentProvider = NewCurrentProvider
			utils.LogDebug("GeoIP: MaxMind provider initialized successfully")
		} else {
			CurrentProvider = &EmptyProvider{}
			utils.LogDebug("GeoIP: Fallback to EmptyProvider due to MaxMind failure")
			utils.LogWarn("Failed to initialize MaxMind GeoIP service, using EmptyProvider instead.")
		}
	case "ip-api":
		utils.LogDebug("GeoIP: Initializing ip-api provider...")
		NewCurrentProvider, err := NewIPAPIService()
		if err != nil {
			utils.LogDebug("GeoIP: ip-api initialization failed: %v", err)
			utils.LogError("Failed to initialize ip-api service: " + err.Error())
		}
		if NewCurrentProvider != nil {
			CurrentProvider = NewCurrentProvider
			utils.LogDebug("GeoIP: ip-api provider initialized successfully")
			utils.LogInfo("Using ip-api.com as GeoIP provider.")
		} else {
			CurrentProvider = &EmptyProvider{}
			utils.LogDebug("GeoIP: Fallback to EmptyProvider due to ip-api failure")
			utils.LogWarn("Failed to initialize ip-api service, using EmptyProvider instead.")
		}
	case "geojs":
		utils.LogDebug("GeoIP: Initializing GeoJS provider...")
		NewCurrentProvider, err := NewGeoJSService()
		if err != nil {
			utils.LogDebug("GeoIP: GeoJS initialization failed: %v", err)
			utils.LogError("Failed to initialize GeoJS service: " + err.Error())
		}
		if NewCurrentProvider != nil {
			CurrentProvider = NewCurrentProvider
			utils.LogDebug("GeoIP: GeoJS provider initialized successfully")
			utils.LogInfo("Using geojs.io as GeoIP provider.")
		} else {
			CurrentProvider = &EmptyProvider{}
			utils.LogDebug("GeoIP: Fallback to EmptyProvider due to GeoJS failure")
			utils.LogWarn("Failed to initialize GeoJS service, using EmptyProvider instead.")
		}
	case "ipinfo":
		utils.LogDebug("GeoIP: Initializing IPInfo provider...")
		NewCurrentProvider, err := NewIPInfoService()
		if err != nil {
			utils.LogDebug("GeoIP: IPInfo initialization failed: %v", err)
			utils.LogError("Failed to initialize IPInfo service: " + err.Error())
		}
		if NewCurrentProvider != nil {
			CurrentProvider = NewCurrentProvider
			utils.LogDebug("GeoIP: IPInfo provider initialized successfully")
			utils.LogInfo("Using ipinfo.io as GeoIP provider.")
		} else {
			CurrentProvider = &EmptyProvider{}
			utils.LogDebug("GeoIP: Fallback to EmptyProvider due to IPInfo failure")
			utils.LogWarn("Failed to initialize IPInfo service, using EmptyProvider instead.")
		}
	default:
		utils.LogDebug("GeoIP: Unknown provider '%s', using EmptyProvider", conf.GeoIpProvider)
		CurrentProvider = &EmptyProvider{}
	}

	utils.LogDebug("GeoIP: Initialization completed with provider: %s", CurrentProvider.Name())
}

func GetGeoInfo(ip net.IP) (*GeoInfo, error) {
	providerName := CurrentProvider.Name()
	cacheKey := providerName + ":" + ip.String()

	utils.LogDebug("GeoIP: Looking up IP %s using provider %s", ip.String(), providerName)

	if cachedInfo, found := geoCache.Get(cacheKey); found {
		utils.LogDebug("GeoIP: Cache hit for %s", cacheKey)
		return cachedInfo.(*GeoInfo), nil
	}

	utils.LogDebug("GeoIP: Cache miss for %s, querying provider...", cacheKey)
	info, err := CurrentProvider.GetGeoInfo(ip)
	if err != nil {
		utils.LogDebug("GeoIP: Provider query failed: %v", err)
		return info, err
	}

	if info != nil {
		utils.LogDebug("GeoIP: Provider returned: ISOCode=%s, Name=%s", info.ISOCode, info.Name)
		geoCache.Set(cacheKey, info, cache.DefaultExpiration)
		utils.LogDebug("GeoIP: Result cached for %s", cacheKey)
	} else {
		utils.LogDebug("GeoIP: Provider returned nil result")
	}

	return info, err
}

func UpdateDatabase() error {
	utils.LogDebug("GeoIP: Updating database for provider: %s", CurrentProvider.Name())
	err := CurrentProvider.UpdateDatabase()
	if err == nil {
		geoCache.Flush()
		utils.LogDebug("GeoIP: Database updated successfully, cache cleared")
		utils.LogInfo("GeoIP cache cleared due to database update.")
	} else {
		utils.LogDebug("GeoIP: Database update failed: %v", err)
	}
	return err
}
