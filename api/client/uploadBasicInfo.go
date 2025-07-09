package client

import (
	"net"

	"github.com/komari-monitor/komari/database/clients"
	"github.com/komari-monitor/komari/database/config"
	"github.com/komari-monitor/komari/utils"
	"github.com/komari-monitor/komari/utils/geoip"

	"github.com/gin-gonic/gin"
)

func UploadBasicInfo(c *gin.Context) {
	utils.LogDebug("Server: Received uploadBasicInfo request")

	var cbi = map[string]interface{}{}
	if err := c.ShouldBindJSON(&cbi); err != nil {
		utils.LogDebug("Server: Failed to parse JSON data: %v", err)
		c.JSON(400, gin.H{"status": "error", "error": "Invalid or missing data"})
		return
	}

	utils.LogDebug("Server: Received client data: %+v", cbi)

	token := c.Query("token")
	utils.LogDebug("Server: Processing token: %s", func() string {
		if len(token) > 8 {
			return token[:8] + "..."
		}
		return token
	}())

	uuid, err := clients.GetClientUUIDByToken(token)
	if uuid == "" || err != nil {
		utils.LogDebug("Server: Token validation failed - UUID='%s', Error=%v", uuid, err)
		c.JSON(400, gin.H{"status": "error", "error": "Invalid token"})
		return
	}

	utils.LogDebug("Server: Token validated successfully, Client UUID: %s", uuid)
	cbi["uuid"] = uuid

	// GeoIP处理阶段
	cfg, configErr := config.Get()
	utils.LogDebug("Server: Config retrieved - GeoIpEnabled=%v, Error=%v", cfg.GeoIpEnabled, configErr)

	if configErr == nil && cfg.GeoIpEnabled {
		utils.LogDebug("Server: Starting GeoIP processing...")

		if ipv4, ok := cbi["ipv4"].(string); ok && ipv4 != "" {
			utils.LogDebug("Server: Processing IPv4 address: %s", ipv4)
			ip4 := net.ParseIP(ipv4)
			if ip4 == nil {
				utils.LogDebug("Server: Invalid IPv4 address format: %s", ipv4)
			} else {
				ip4_record, geoErr := geoip.GetGeoInfo(ip4)
				utils.LogDebug("Server: GeoIP IPv4 lookup result - Record=%+v, Error=%v", ip4_record, geoErr)
				if ip4_record != nil {
					emoji := geoip.GetRegionUnicodeEmoji(ip4_record.ISOCode)
					cbi["region"] = emoji
					utils.LogDebug("Server: Set region from IPv4 - ISOCode='%s', Emoji='%s'", ip4_record.ISOCode, emoji)
				}
			}
		} else if ipv6, ok := cbi["ipv6"].(string); ok && ipv6 != "" {
			utils.LogDebug("Server: Processing IPv6 address: %s", ipv6)
			ip6 := net.ParseIP(ipv6)
			if ip6 == nil {
				utils.LogDebug("Server: Invalid IPv6 address format: %s", ipv6)
			} else {
				ip6_record, geoErr := geoip.GetGeoInfo(ip6)
				utils.LogDebug("Server: GeoIP IPv6 lookup result - Record=%+v, Error=%v", ip6_record, geoErr)
				if ip6_record != nil {
					emoji := geoip.GetRegionUnicodeEmoji(ip6_record.ISOCode)
					cbi["region"] = emoji
					utils.LogDebug("Server: Set region from IPv6 - ISOCode='%s', Emoji='%s'", ip6_record.ISOCode, emoji)
				}
			}
		} else {
			utils.LogDebug("Server: No valid IP address found for GeoIP lookup")
		}
	} else {
		utils.LogDebug("Server: GeoIP disabled or config error, skipping GeoIP processing")
	}

	utils.LogDebug("Server: Final data before saving: %+v", cbi)

	if err := clients.SaveClientInfo(cbi); err != nil {
		utils.LogDebug("Server: Failed to save client info: %v", err)
		c.JSON(500, gin.H{"status": "error", "error": err})
		return
	}

	utils.LogDebug("Server: Client info saved successfully for UUID: %s", uuid)
	c.JSON(200, gin.H{"status": "success"})
}
