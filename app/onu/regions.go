package onu

// Region is a China Mobile config profile selectable through the device shell
// with `upgradetest sdefconf <ID>`.
type Region struct {
	ID   int
	Name string
}

// Regions lists the known sdefconf profiles in the reference order. The IDs are
// passed verbatim to `upgradetest sdefconf`; the names carry the English label
// and its Chinese province for the UI.
var Regions = []Region{
	{400, "Jiangsu 江苏"},
	{401, "Xinjiang 新疆"},
	{402, "Hainan 海南"},
	{403, "Tianjin 天津"},
	{404, "Anhui 安徽"},
	{405, "Shanghai 上海"},
	{406, "Chongqing 重庆"},
	{407, "Beijing 北京"},
	{408, "Sichuan 四川"},
	{409, "Shandong 山东"},
	{410, "Guangdong 广东"},
	{411, "Hubei 湖北"},
	{412, "Fujian 福建"},
	{413, "Suzhou 苏州"},
	{414, "Zhejiang 浙江"},
	{415, "Shanxi 山西"},
	{416, "Hunan 湖南"},
	{417, "Yunnan 云南"},
	{418, "Xizang 西藏"},
	{419, "Heilongjiang 黑龙江"},
	{420, "Guizhou 贵州"},
	{421, "Shanxi2 陕西"},
	{422, "Hebei 河北"},
	{423, "Ningxia 宁夏"},
	{424, "Guangxi 广西"},
	{425, "Jiangxi 江西"},
	{426, "Gansu 甘肃"},
	{427, "Qinghai 青海"},
	{428, "Xian 西安"},
	{429, "Liaoning 辽宁"},
	{430, "Jilin 吉林"},
	{431, "Neimeng 内蒙古"},
	{432, "Henan 河南"},
	{440, "Guangxi660 广西660"},
	{497, "IPv6ready"},
	{498, "Regioncommon 通用"},
	{466, "Jicai 集采"},
}

// DefaultRegionID is the region preselected in the UI (Henan).
const DefaultRegionID = 432

// RegionIndexByID returns the index of the region with the given ID in Regions,
// or 0 when it is not found.
func RegionIndexByID(id int) int {
	for i, r := range Regions {
		if r.ID == id {
			return i
		}
	}
	return 0
}
