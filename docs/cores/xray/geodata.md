# Xray GeoData

FastV uses Xray GeoData files for routing rules such as:

- `geoip:private`
- `geoip:cn`
- `geosite:cn`
- `geosite:category-ads-all`

## Files

Standard files:

| 文件 | 用途 |
|---|---|
| `geoip.dat` | GeoIP 规则数据 |
| `geosite.dat` | GeoSite 域名规则数据 |

FastV also recognizes additional `*.dat` files in the Xray core directory.

## Location

GeoData is stored next to `xray.exe`:

```text
<FastV data directory>/
  cores/
    xray/
      xray.exe
      geoip.dat
      geosite.dat
      wintun.dll
```

When starting Xray, FastV sets:

```text
XRAY_LOCATION_ASSET=<xray core directory>
```

This ensures routing rules consistently resolve GeoData from the installed core directory.

## Settings

Settings -> 内核设置 -> GeoData 资源 shows:

- GeoIP file presence and size
- GeoSite file presence and size
- Number of `.dat` files
- Resource directory
- Refresh status
- Download / update GeoData

Updating GeoData downloads the latest Xray release archive, verifies its SHA-256 digest, and copies only `*.dat` files. The Xray executable is not replaced.

## Routing Rules

The routing editor maps:

```text
GeoIP rule   -> ip: ["geoip:<value>"]
GeoSite rule -> domain: ["geosite:<value>"]
```

If the required `.dat` files are missing, Xray may fail to resolve these rules or fail configuration validation.

## Notes

- GeoData updates require network access to the Xray release source.
- GeoData updates are independent from Xray binary updates.
- The API and stats settings do not change GeoData behavior.