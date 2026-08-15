package demo

import (
	"github.com/Paraview-RD/portico/internal/model"
	"github.com/Paraview-RD/portico/internal/service"
)

// The industry packs.
//
// Four industries and one generic world. They are not one shape with four sets
// of names: a hospital's departments are wide and flat where a bank's branches
// are deep, a factory records a shift where a university records whether
// somebody is staff or a student, and a campus signs people into CAS services
// where a bank signs them into SAML ones. A pack that differed only in
// vocabulary would teach a visitor that Portico has one shape and they must
// adopt it, which is the opposite of true — so packs_test.go asserts the
// differences rather than trusting this comment.
//
// Sizes are bounded by a fact outside this file: a pack is created inside the
// HTTP request that confirms a trial, and every account costs one bcrypt hash.
// Fifteen accounts is about a second on a developer's machine and several on
// the small instance a public demonstration runs on. Paging — which is why
// internal/seed makes fifty-five — is not what a trial is for.

// Pack is one industry's world.
type Pack struct {
	// Key is what the form submits and the trial_requests row stores.
	Key string
	// Label is for this package's own tests and documentation. What a visitor
	// reads is translated in the console; a Chinese string here would arrive in
	// an English interface untranslated.
	Label string

	Orgs       []Org
	People     []Person
	Groups     []Group
	Attributes []Attribute

	OAuth []OAuthApp
	SAML  []SAMLApp
	CAS   []CASApp

	// Manager is who runs a department, and OrgAdmin is who is recorded as
	// administering one. Different facts, and deliberately different people:
	// a pack where they are always the same account teaches that they are the
	// same thing.
	Manager  Assignment
	OrgAdmin Assignment
}

// Org is one node of the tree, named by the key later entries refer to it by.
type Org struct {
	Key, Name, Code, Parent, Remark string
	// Disabled exercises the state where the people already in it stay and no
	// new ones may be assigned.
	Disabled bool
}

// Person is one account. Every one is an ordinary user; see fillPeople.
type Person struct {
	Username, DisplayName, Email, Phone, Org string
	Source                                   model.UserSource
	Disabled                                 bool
	// NoContact is an account with neither address nor phone number. The portal
	// warns about it, because such an account cannot recover its own password,
	// and that warning is invisible without one.
	NoContact bool
}

// Group is a list of people, from one of the two places groups come from.
type Group struct {
	Name, Description, ExternalID string
	Source                        model.GroupSource
	Members                       []string
}

// Attribute is a fact this tenant decided to record about people.
type Attribute struct {
	Key, Label, Description, Kind string
	Allowed                       []string
	// FilledFor is the accounts that have a value, and what it is. Not
	// everybody, deliberately — see fillAttributes.
	FilledFor map[string]string
}

// OAuthApp is a registered OAuth client. Type is one of model's application
// types, and Public marks the ones that cannot hold a secret.
type OAuthApp struct {
	ClientID, Name, Type, Launch string
	Public, Disabled             bool
	Redirect, Scopes             []string
}

// SAMLApp is a service provider. Registered from generated metadata, the way
// the console registers one: by pasting a document rather than filling fields.
type SAMLApp struct {
	Name, EntityID, Host, Launch string
}

// CASApp is a CAS service, matched by URL prefix.
type CASApp struct {
	Name, Prefix, Launch string
	Disabled             bool
}

// Assignment names an account and an organization, and for an administrator
// the scope the record covers.
type Assignment struct {
	Org, Username, Scope string
}

// packByKey finds a pack, or nil.
func packByKey(key string) *Pack {
	for i := range packs {
		if packs[i].Key == key {
			return &packs[i]
		}
	}
	return nil
}

// packs is the offer, in the order the form shows it. Generic first, because
// it is the safe answer for somebody who does not see their own industry.
var packs = []Pack{genericPack, manufacturingPack, bankingPack, hospitalPack, universityPack}

// IndustryGeneric is the pack a request gets when it names none.
const IndustryGeneric = "generic"

// A company that could be anything, for a visitor whose industry is not on the
// list. Two levels and one second root, which is the shallowest tree that is
// still a tree and still shows that a tenant may have more than one.
var genericPack = Pack{
	Key: IndustryGeneric, Label: "General business",
	Orgs: []Org{
		{Key: "hq", Name: "总部", Code: "HQ", Remark: "顶级组织"},
		{Key: "tech", Name: "技术部", Code: "TECH", Parent: "hq"},
		{Key: "sales", Name: "销售部", Code: "SALES", Parent: "hq"},
		{Key: "ops", Name: "运营部", Code: "OPS", Parent: "hq"},
		{Key: "external", Name: "外部协作", Code: "EXT", Remark: "承包商与顾问"},
		{Key: "legacy", Name: "行政部（已并入总部）", Code: "ADMIN", Parent: "hq",
			Remark: "保留以便查阅历史", Disabled: true},
	},
	People: []Person{
		{Username: "zhangwei", DisplayName: "张伟", Email: "zhangwei@example.com",
			Phone: "13800000001", Org: "tech", Source: model.SourceAdmin},
		{Username: "liyan", DisplayName: "李燕", Email: "liyan@example.com",
			Phone: "13800000002", Org: "tech", Source: model.SourceAdmin},
		{Username: "wangfang", DisplayName: "王芳", Email: "wangfang@example.com",
			Phone: "13800000003", Org: "tech", Source: model.SourceImport},
		{Username: "chenjing", DisplayName: "陈静", Email: "chenjing@example.com",
			Org: "sales", Source: model.SourceAdmin},
		{Username: "zhaolei", DisplayName: "赵磊", Email: "zhaolei@example.com",
			Phone: "13800000005", Org: "sales", Source: model.SourceSCIM},
		{Username: "sunli", DisplayName: "孙丽", Email: "sunli@example.com",
			Org: "sales", Source: model.SourceRegistration},
		{Username: "zhouming", DisplayName: "周明", Email: "zhouming@example.com",
			Phone: "13800000007", Org: "ops", Source: model.SourceAdmin},
		{Username: "wuqiang", DisplayName: "吴强", Email: "wuqiang@example.com",
			Org: "ops", Source: model.SourceLDAP},
		{Username: "zhengna", DisplayName: "郑娜", Email: "zhengna@example.com",
			Phone: "13800000009", Org: "ops", Source: model.SourceImport},
		{Username: "mei.tanaka", DisplayName: "Mei Tanaka", Email: "mei@example.com",
			Org: "external", Source: model.SourceSCIM},
		{Username: "long.name", DisplayName: "欧阳建国·亚历山德罗·冯·穆勒-施密特",
			Email: "long@example.com", Org: "external", Source: model.SourceImport},
		{Username: "no.contact", DisplayName: "无联系方式", Org: "external",
			Source: model.SourceImport, NoContact: true},
		{Username: "left.company", DisplayName: "离职员工", Email: "left@example.com",
			Org: "legacy", Source: model.SourceAdmin, Disabled: true},
	},
	Groups: []Group{
		{Name: "值班工程师", Description: "轮值处理线上问题", Source: model.GroupSourceAdmin,
			Members: []string{"zhangwei", "liyan", "wangfang"}},
		{Name: "All Staff", Description: "由目录推送", Source: model.GroupSourceSCIM,
			ExternalID: "grp-all-staff",
			Members:    []string{"zhangwei", "chenjing", "zhouming", "mei.tanaka"}},
	},
	Attributes: []Attribute{
		{Key: "badge_number", Label: "门禁卡号",
			Description: "园区门禁系统里的卡号，随人走而不随工号走",
			Kind:        service.FieldKindText,
			FilledFor: map[string]string{
				"zhangwei": "A-10293", "liyan": "A-10294", "wangfang": "A-11007",
			}},
		{Key: "work_mode", Label: "办公方式", Description: "用于下游的座位与设备发放",
			Kind: service.FieldKindSelect, Allowed: []string{"ONSITE", "HYBRID", "REMOTE"},
			FilledFor: map[string]string{
				"zhangwei": "ONSITE", "chenjing": "HYBRID", "mei.tanaka": "REMOTE",
			}},
		{Key: "joined_on", Label: "入职日期", Description: "用于工龄与权限回收的起算",
			Kind: service.FieldKindDate,
			FilledFor: map[string]string{
				"zhangwei": "2021-03-15", "zhouming": "2023-09-01",
			}},
	},
	OAuth: []OAuthApp{
		{ClientID: "wiki", Name: "内部 Wiki", Type: model.AppTypeWeb,
			Redirect: []string{"https://wiki.example.com/oauth2/callback"},
			Scopes:   []string{"openid", "profile", "email"},
			Launch:   "https://wiki.example.com/"},
		{ClientID: "staff-app", Name: "员工 App", Type: model.AppTypeNative, Public: true,
			Redirect: []string{"com.example.portico://callback"},
			Scopes:   []string{"openid", "profile", "offline_access"}},
	},
	SAML: []SAMLApp{
		{Name: "报销系统（SAML）", EntityID: "https://expense.example.com/saml/metadata",
			Host: "expense.example.com", Launch: "https://expense.example.com/"},
	},
	Manager:  Assignment{Org: "tech", Username: "zhangwei"},
	OrgAdmin: Assignment{Org: "sales", Username: "chenjing", Scope: model.OrgScopeSubtree},
}

// A factory group. Wide at the plant level and shallow below it, which is what
// a site-and-line organization looks like: the interesting boundary is between
// plants, not between levels of management.
var manufacturingPack = Pack{
	Key: "manufacturing", Label: "Manufacturing",
	Orgs: []Org{
		{Key: "group", Name: "集团总部", Code: "GRP"},
		{Key: "east", Name: "华东厂区", Code: "PLANT-E", Parent: "group",
			Remark: "主力厂区，三班倒"},
		{Key: "assembly", Name: "总装车间", Code: "E-ASM", Parent: "east"},
		{Key: "stamping", Name: "冲压车间", Code: "E-STP", Parent: "east"},
		{Key: "quality", Name: "质量中心", Code: "QA", Parent: "group",
			Remark: "跨厂区，直属集团"},
		{Key: "supply", Name: "供应链", Code: "SCM", Parent: "group"},
		{Key: "south", Name: "华南厂区（已关停）", Code: "PLANT-S", Parent: "group",
			Remark: "2025 年并入华东，人员档案保留", Disabled: true},
	},
	People: []Person{
		{Username: "linjianguo", DisplayName: "林建国", Email: "lin.jg@mfg.example.com",
			Phone: "13910000001", Org: "east", Source: model.SourceAdmin},
		{Username: "hexiaoming", DisplayName: "何晓明", Email: "he.xm@mfg.example.com",
			Phone: "13910000002", Org: "assembly", Source: model.SourceAdmin},
		{Username: "guoping", DisplayName: "郭平", Email: "guo.p@mfg.example.com",
			Org: "assembly", Source: model.SourceLDAP},
		{Username: "xuhaiyan", DisplayName: "徐海燕", Email: "xu.hy@mfg.example.com",
			Phone: "13910000004", Org: "assembly", Source: model.SourceLDAP},
		{Username: "machao", DisplayName: "马超", Email: "ma.c@mfg.example.com",
			Org: "stamping", Source: model.SourceImport},
		{Username: "dengli", DisplayName: "邓丽", Email: "deng.l@mfg.example.com",
			Phone: "13910000006", Org: "stamping", Source: model.SourceLDAP},
		{Username: "fengyu", DisplayName: "冯宇", Org: "stamping",
			Source: model.SourceImport, NoContact: true},
		{Username: "tangwei", DisplayName: "唐伟", Email: "tang.w@mfg.example.com",
			Phone: "13910000008", Org: "quality", Source: model.SourceAdmin},
		{Username: "yaolin", DisplayName: "姚琳", Email: "yao.l@mfg.example.com",
			Org: "quality", Source: model.SourceAdmin},
		{Username: "shenbin", DisplayName: "沈斌", Email: "shen.b@mfg.example.com",
			Phone: "13910000010", Org: "quality", Source: model.SourceSCIM},
		{Username: "cuiyan", DisplayName: "崔燕", Email: "cui.y@mfg.example.com",
			Org: "supply", Source: model.SourceLDAP},
		{Username: "panjun", DisplayName: "潘军", Email: "pan.j@mfg.example.com",
			Phone: "13910000012", Org: "supply", Source: model.SourceImport},
		{Username: "shiyong", DisplayName: "石勇", Email: "shi.y@mfg.example.com",
			Org: "supply", Source: model.SourceSCIM},
		{Username: "gone.from.south", DisplayName: "华南厂区留档", Email: "south@mfg.example.com",
			Org: "south", Source: model.SourceLDAP, Disabled: true},
	},
	Groups: []Group{
		{Name: "夜班班长", Description: "夜班期间的现场决定权", Source: model.GroupSourceAdmin,
			Members: []string{"guoping", "machao", "dengli"}},
		{Name: "质量放行人", Description: "有权签发出厂检验单", Source: model.GroupSourceAdmin,
			Members: []string{"tangwei", "yaolin"}},
		{Name: "Plant East", Description: "由 AD 按厂区推送", Source: model.GroupSourceSCIM,
			ExternalID: "grp-plant-east",
			Members:    []string{"linjianguo", "hexiaoming", "guoping", "xuhaiyan", "machao"}},
	},
	Attributes: []Attribute{
		{Key: "employee_no", Label: "工号",
			Description: "人事系统主键，与门禁、考勤、工时都对得上",
			Kind:        service.FieldKindText,
			FilledFor: map[string]string{
				"linjianguo": "MFG-000117", "hexiaoming": "MFG-002045",
				"guoping": "MFG-004410", "tangwei": "MFG-001902",
			}},
		{Key: "shift", Label: "班次", Description: "决定考勤规则与津贴，按周轮换",
			Kind: service.FieldKindSelect, Allowed: []string{"DAY", "SWING", "NIGHT"},
			FilledFor: map[string]string{
				"guoping": "NIGHT", "xuhaiyan": "SWING", "machao": "NIGHT",
				"dengli": "DAY",
			}},
		{Key: "safety_cert_expiry", Label: "安全资格到期",
			Description: "到期未复训的人不得进入车间，一个已经过期的日期是常态而不是错误",
			Kind:        service.FieldKindDate,
			FilledFor: map[string]string{
				// One already past, on purpose: the expired case is the one an
				// operator has to be able to spot, and a pack where every date
				// is in the future never shows it.
				"guoping": "2026-05-30", "machao": "2027-02-14", "dengli": "2026-11-01",
			}},
	},
	OAuth: []OAuthApp{
		{ClientID: "mes", Name: "MES 制造执行系统", Type: model.AppTypeWeb,
			Redirect: []string{"https://mes.mfg.example.com/oidc/callback"},
			Scopes:   []string{"openid", "profile", "groups"},
			Launch:   "https://mes.mfg.example.com/"},
		{ClientID: "andon-board", Name: "车间安灯看板", Type: model.AppTypeUserAgent, Public: true,
			Redirect: []string{"https://andon.mfg.example.com/callback"},
			Scopes:   []string{"openid", "profile"},
			Launch:   "https://andon.mfg.example.com/"},
	},
	SAML: []SAMLApp{
		{Name: "ERP（SAML）", EntityID: "https://erp.mfg.example.com/saml/metadata",
			Host: "erp.mfg.example.com", Launch: "https://erp.mfg.example.com/"},
	},
	CAS: []CASApp{
		{Name: "设备管理（CAS，已停用）", Prefix: "https://eam.mfg.example.com/",
			Disabled: true},
	},
	Manager:  Assignment{Org: "east", Username: "linjianguo"},
	OrgAdmin: Assignment{Org: "quality", Username: "tangwei", Scope: model.OrgScopeSelf},
}

// A bank. Deep rather than wide: head office, business line, branch, sub-branch
// — four levels, which is the shape that makes an organization tree worth
// having and the one where a subtree-scoped administrator finally means
// something.
var bankingPack = Pack{
	Key: "banking", Label: "Banking",
	Orgs: []Org{
		{Key: "head", Name: "总行", Code: "HO"},
		{Key: "retail", Name: "零售银行部", Code: "RB", Parent: "head"},
		{Key: "shanghai", Name: "上海分行", Code: "RB-SH", Parent: "retail"},
		{Key: "jingan", Name: "静安支行", Code: "RB-SH-JA", Parent: "shanghai"},
		{Key: "hongkou", Name: "虹口支行", Code: "RB-SH-HK", Parent: "shanghai"},
		{Key: "corporate", Name: "公司银行部", Code: "CB", Parent: "head"},
		{Key: "risk", Name: "风险管理部", Code: "RISK", Parent: "head",
			Remark: "独立汇报线，不隶属业务条线"},
		{Key: "closed", Name: "闸北支行（已撤并）", Code: "RB-SH-ZB", Parent: "shanghai",
			Remark: "2024 年撤并入静安支行", Disabled: true},
	},
	People: []Person{
		{Username: "yuandong", DisplayName: "袁东", Email: "yuan.d@bank.example.com",
			Phone: "13710000001", Org: "head", Source: model.SourceAdmin},
		{Username: "qiaoshan", DisplayName: "乔珊", Email: "qiao.s@bank.example.com",
			Org: "retail", Source: model.SourceAdmin},
		{Username: "luyifan", DisplayName: "陆一凡", Email: "lu.yf@bank.example.com",
			Phone: "13710000003", Org: "shanghai", Source: model.SourceLDAP},
		{Username: "hantong", DisplayName: "韩彤", Email: "han.t@bank.example.com",
			Org: "jingan", Source: model.SourceLDAP},
		{Username: "baiyu", DisplayName: "白宇", Email: "bai.y@bank.example.com",
			Phone: "13710000005", Org: "jingan", Source: model.SourceLDAP},
		{Username: "kongxiang", DisplayName: "孔祥", Email: "kong.x@bank.example.com",
			Org: "jingan", Source: model.SourceImport},
		{Username: "leiming", DisplayName: "雷鸣", Email: "lei.m@bank.example.com",
			Phone: "13710000007", Org: "hongkou", Source: model.SourceLDAP},
		{Username: "duanqing", DisplayName: "段青", Email: "duan.q@bank.example.com",
			Org: "hongkou", Source: model.SourceLDAP},
		{Username: "yanru", DisplayName: "阎茹", Email: "yan.r@bank.example.com",
			Phone: "13710000009", Org: "corporate", Source: model.SourceAdmin},
		{Username: "moshaowen", DisplayName: "莫少文", Email: "mo.sw@bank.example.com",
			Org: "corporate", Source: model.SourceSCIM},
		{Username: "jiangbo", DisplayName: "姜波", Email: "jiang.b@bank.example.com",
			Phone: "13710000011", Org: "risk", Source: model.SourceAdmin},
		{Username: "wenjia", DisplayName: "温佳", Email: "wen.j@bank.example.com",
			Org: "risk", Source: model.SourceAdmin},
		{Username: "auditor.readonly", DisplayName: "外部审计（只读）",
			Email: "audit@audit.example.org", Org: "risk", Source: model.SourceRegistration},
		{Username: "transferred.out", DisplayName: "已调离", Email: "left@bank.example.com",
			Org: "closed", Source: model.SourceLDAP, Disabled: true},
	},
	Groups: []Group{
		{Name: "授权复核人", Description: "大额交易的第二双眼睛，双人复核的另一半",
			Source: model.GroupSourceAdmin, Members: []string{"qiaoshan", "luyifan", "jiangbo"}},
		{Name: "反洗钱岗", Description: "可查询可疑交易名单", Source: model.GroupSourceAdmin,
			Members: []string{"jiangbo", "wenjia"}},
		{Name: "Shanghai Branch", Description: "由 HR 系统按机构推送",
			Source: model.GroupSourceSCIM, ExternalID: "grp-branch-sh",
			Members: []string{"luyifan", "hantong", "baiyu", "kongxiang", "leiming", "duanqing"}},
	},
	Attributes: []Attribute{
		{Key: "teller_id", Label: "柜员号",
			Description: "核心系统里的操作员编号，与工号不是一回事",
			Kind:        service.FieldKindText,
			FilledFor: map[string]string{
				"hantong": "SH-JA-0112", "baiyu": "SH-JA-0117",
				"leiming": "SH-HK-0203",
			}},
		{Key: "risk_tier", Label: "授权额度等级",
			Description: "决定单笔可授权金额，调整需风险管理部批准",
			Kind:        service.FieldKindSelect, Allowed: []string{"T1", "T2", "T3", "T4"},
			FilledFor: map[string]string{
				"qiaoshan": "T4", "luyifan": "T3", "hantong": "T1", "leiming": "T2",
			}},
		{Key: "authorised_until", Label: "授权有效期",
			Description: "到期后柜员号自动失效，需重新授权",
			Kind:        service.FieldKindDate,
			FilledFor: map[string]string{
				"hantong": "2027-01-31", "baiyu": "2026-09-30",
			}},
	},
	OAuth: []OAuthApp{
		{ClientID: "ebank-console", Name: "网银运营后台", Type: model.AppTypeWeb,
			Redirect: []string{"https://ops.bank.example.com/oauth2/callback"},
			Scopes:   []string{"openid", "profile", "email", "groups"},
			Launch:   "https://ops.bank.example.com/"},
		{ClientID: "risk-portal", Name: "风控平台", Type: model.AppTypeWeb,
			Redirect: []string{"https://risk.bank.example.com/callback"},
			Scopes:   []string{"openid", "profile", "groups"},
			Launch:   "https://risk.bank.example.com/"},
		{ClientID: "legacy-teller", Name: "旧柜面客户端（已停用）", Type: model.AppTypeNative,
			Redirect: []string{"http://127.0.0.1:9000/callback"},
			Scopes:   []string{"openid", "profile"}, Disabled: true},
	},
	SAML: []SAMLApp{
		{Name: "核心业务系统（SAML）", EntityID: "https://core.bank.example.com/saml/metadata",
			Host: "core.bank.example.com", Launch: "https://core.bank.example.com/"},
	},
	Manager: Assignment{Org: "shanghai", Username: "luyifan"},
	// Subtree scope, which is the one this tree can actually show: the branch
	// has sub-branches under it, so "this organization and everything below"
	// covers people the record does not name.
	OrgAdmin: Assignment{Org: "shanghai", Username: "qiaoshan", Scope: model.OrgScopeSubtree},
}

// A hospital. Wide and flat: a dozen departments reporting to one site, with
// nothing between them — which is the shape most clinical organizations
// actually have, and the one where groups do the work a tree cannot.
var hospitalPack = Pack{
	Key: "hospital", Label: "Hospital",
	Orgs: []Org{
		{Key: "main", Name: "院本部", Code: "MAIN"},
		{Key: "internal", Name: "内科", Code: "INT", Parent: "main"},
		{Key: "surgery", Name: "外科", Code: "SUR", Parent: "main"},
		{Key: "emergency", Name: "急诊科", Code: "ER", Parent: "main",
			Remark: "24 小时，排班与其它科室不同"},
		{Key: "imaging", Name: "医学影像科", Code: "RAD", Parent: "main"},
		{Key: "pharmacy", Name: "药剂科", Code: "PHA", Parent: "main"},
		{Key: "nursing", Name: "护理部", Code: "NUR", Parent: "main"},
		{Key: "admin", Name: "行政后勤", Code: "ADM", Parent: "main"},
		{Key: "branch", Name: "宁河院区（筹）", Code: "BR-NH",
			Remark: "尚未开诊，先建档", Disabled: true},
	},
	People: []Person{
		{Username: "shenyuqing", DisplayName: "沈玉清", Email: "shen.yq@hosp.example.com",
			Phone: "13610000001", Org: "internal", Source: model.SourceAdmin},
		{Username: "luowenbo", DisplayName: "罗文博", Email: "luo.wb@hosp.example.com",
			Org: "internal", Source: model.SourceLDAP},
		{Username: "jiangyue", DisplayName: "蒋玥", Email: "jiang.y@hosp.example.com",
			Phone: "13610000003", Org: "surgery", Source: model.SourceLDAP},
		{Username: "songkai", DisplayName: "宋凯", Email: "song.k@hosp.example.com",
			Org: "surgery", Source: model.SourceLDAP},
		{Username: "yeqian", DisplayName: "叶倩", Email: "ye.q@hosp.example.com",
			Phone: "13610000005", Org: "emergency", Source: model.SourceAdmin},
		{Username: "fanzhihao", DisplayName: "范志豪", Email: "fan.zh@hosp.example.com",
			Org: "emergency", Source: model.SourceLDAP},
		{Username: "qiuxin", DisplayName: "邱欣", Org: "emergency",
			Source: model.SourceImport, NoContact: true},
		{Username: "biyunfei", DisplayName: "毕云飞", Email: "bi.yf@hosp.example.com",
			Phone: "13610000008", Org: "imaging", Source: model.SourceLDAP},
		{Username: "tanmeng", DisplayName: "谭萌", Email: "tan.m@hosp.example.com",
			Org: "pharmacy", Source: model.SourceLDAP},
		{Username: "houjing", DisplayName: "侯静", Email: "hou.j@hosp.example.com",
			Phone: "13610000010", Org: "nursing", Source: model.SourceSCIM},
		{Username: "kangxiaoyu", DisplayName: "康小雨", Email: "kang.xy@hosp.example.com",
			Org: "nursing", Source: model.SourceSCIM},
		{Username: "guyan", DisplayName: "顾岩", Email: "gu.y@hosp.example.com",
			Phone: "13610000012", Org: "nursing", Source: model.SourceSCIM},
		{Username: "weichun", DisplayName: "魏春", Email: "wei.c@hosp.example.com",
			Org: "admin", Source: model.SourceAdmin},
		{Username: "retired.doctor", DisplayName: "已退休（返聘中止）",
			Email: "retired@hosp.example.com", Org: "internal",
			Source: model.SourceLDAP, Disabled: true},
	},
	Groups: []Group{
		{Name: "总值班", Description: "夜间与节假日的全院决定权", Source: model.GroupSourceAdmin,
			Members: []string{"shenyuqing", "jiangyue", "yeqian"}},
		{Name: "处方权", Description: "可开具处方，与执业证绑定", Source: model.GroupSourceAdmin,
			Members: []string{"shenyuqing", "luowenbo", "jiangyue", "songkai", "yeqian"}},
		{Name: "Nursing Staff", Description: "由 HR 按职系推送", Source: model.GroupSourceSCIM,
			ExternalID: "grp-nursing",
			Members:    []string{"houjing", "kangxiaoyu", "guyan"}},
	},
	Attributes: []Attribute{
		{Key: "license_no", Label: "执业证号",
			Description: "卫健委执业注册号，处方权与它绑定",
			Kind:        service.FieldKindText,
			FilledFor: map[string]string{
				"shenyuqing": "1101202100341", "luowenbo": "1101202200876",
				"jiangyue": "1101201900412", "yeqian": "1101202000155",
			}},
		{Key: "clinical_role", Label: "医护类别",
			Description: "决定门户里看到哪些系统，与组织无关",
			Kind:        service.FieldKindSelect,
			Allowed:     []string{"DOCTOR", "NURSE", "TECHNICIAN", "ADMIN"},
			FilledFor: map[string]string{
				"shenyuqing": "DOCTOR", "jiangyue": "DOCTOR", "houjing": "NURSE",
				"kangxiaoyu": "NURSE", "biyunfei": "TECHNICIAN", "weichun": "ADMIN",
			}},
		{Key: "credential_review_on", Label: "下次考核日期",
			Description: "定期考核未通过将暂停执业",
			Kind:        service.FieldKindDate,
			FilledFor: map[string]string{
				"shenyuqing": "2027-03-31", "jiangyue": "2026-12-15",
			}},
	},
	OAuth: []OAuthApp{
		{ClientID: "pacs", Name: "PACS 影像系统", Type: model.AppTypeWeb,
			Redirect: []string{"https://pacs.hosp.example.com/oidc/callback"},
			Scopes:   []string{"openid", "profile", "groups"},
			Launch:   "https://pacs.hosp.example.com/"},
	},
	SAML: []SAMLApp{
		{Name: "电子病历（SAML）", EntityID: "https://emr.hosp.example.com/saml/metadata",
			Host: "emr.hosp.example.com", Launch: "https://emr.hosp.example.com/"},
	},
	CAS: []CASApp{
		{Name: "HIS 住院医生站（CAS）", Prefix: "https://his.hosp.example.com/",
			Launch: "https://his.hosp.example.com/"},
		{Name: "药房管理（CAS，已停用）", Prefix: "https://pharmacy.hosp.example.com/",
			Disabled: true},
	},
	Manager:  Assignment{Org: "emergency", Username: "yeqian"},
	OrgAdmin: Assignment{Org: "nursing", Username: "houjing", Scope: model.OrgScopeSelf},
}

// A university. Two roots rather than one, because teaching and administration
// are genuinely not under each other — the shape a tenant has when it is two
// organizations sharing a directory, which is where "an organization may have
// no parent" stops being an edge case.
var universityPack = Pack{
	Key: "university", Label: "University",
	Orgs: []Org{
		{Key: "academic", Name: "教学单位", Code: "ACAD", Remark: "学院与系"},
		{Key: "cs", Name: "计算机学院", Code: "CS", Parent: "academic"},
		{Key: "math", Name: "数学学院", Code: "MATH", Parent: "academic"},
		{Key: "fl", Name: "外国语学院", Code: "FL", Parent: "academic"},
		{Key: "admin", Name: "行政单位", Code: "ADMIN", Remark: "机关与直属单位"},
		{Key: "registry", Name: "教务处", Code: "REG", Parent: "admin"},
		{Key: "library", Name: "图书馆", Code: "LIB", Parent: "admin"},
		{Key: "ce", Name: "继续教育学院（停止招生）", Code: "CE", Parent: "academic",
			Remark: "在读学生毕业前保留", Disabled: true},
	},
	People: []Person{
		{Username: "shangguanrui", DisplayName: "上官睿", Email: "sgr@edu.example.com",
			Phone: "13510000001", Org: "cs", Source: model.SourceAdmin},
		{Username: "muhaoran", DisplayName: "穆浩然", Email: "mhr@edu.example.com",
			Org: "cs", Source: model.SourceLDAP},
		{Username: "s2024010", DisplayName: "student-2024010", Email: "s2024010@edu.example.com",
			Org: "cs", Source: model.SourceSCIM},
		{Username: "s2024011", DisplayName: "student-2024011", Email: "s2024011@edu.example.com",
			Org: "cs", Source: model.SourceSCIM},
		{Username: "s2023077", DisplayName: "student-2023077", Email: "s2023077@edu.example.com",
			Org: "math", Source: model.SourceSCIM},
		{Username: "qinshuang", DisplayName: "秦爽", Email: "qs@edu.example.com",
			Phone: "13510000006", Org: "math", Source: model.SourceLDAP},
		{Username: "yinzhe", DisplayName: "尹哲", Email: "yz@edu.example.com",
			Org: "math", Source: model.SourceLDAP},
		{Username: "tomas.novak", DisplayName: "Tomáš Novák", Email: "novak@edu.example.com",
			Org: "fl", Source: model.SourceLDAP},
		{Username: "buyunxi", DisplayName: "卜韵溪", Email: "byx@edu.example.com",
			Phone: "13510000009", Org: "fl", Source: model.SourceAdmin},
		{Username: "louzhen", DisplayName: "娄真", Email: "lz@edu.example.com",
			Org: "registry", Source: model.SourceAdmin},
		{Username: "yuwenqi", DisplayName: "宇文琪", Email: "ywq@edu.example.com",
			Phone: "13510000011", Org: "registry", Source: model.SourceAdmin},
		{Username: "chuhang", DisplayName: "褚航", Email: "ch@edu.example.com",
			Org: "library", Source: model.SourceImport},
		{Username: "visiting.scholar", DisplayName: "访问学者（学期制）",
			Email: "visitor@edu.example.com", Org: "fl", Source: model.SourceRegistration},
		{Username: "graduated", DisplayName: "已毕业", Email: "alumni@edu.example.com",
			Org: "ce", Source: model.SourceSCIM, Disabled: true},
	},
	Groups: []Group{
		{Name: "研究生导师", Description: "可查看所指导学生的选课与成绩",
			Source:  model.GroupSourceAdmin,
			Members: []string{"shangguanrui", "qinshuang", "buyunxi"}},
		{Name: "Students", Description: "由学工系统按学籍推送", Source: model.GroupSourceSCIM,
			ExternalID: "grp-students",
			Members:    []string{"s2024010", "s2024011", "s2023077"}},
		{Name: "Library Access", Description: "由图书馆系统推送，含校外读者",
			Source: model.GroupSourceSCIM, ExternalID: "grp-library",
			Members: []string{"chuhang", "visiting.scholar", "s2024010"}},
	},
	Attributes: []Attribute{
		{Key: "campus_id", Label: "一卡通号",
			Description: "教工号或学号，全校唯一，与用户名不同",
			Kind:        service.FieldKindText,
			FilledFor: map[string]string{
				"shangguanrui": "T20180231", "qinshuang": "T20150118",
				"s2024010": "2024010", "s2024011": "2024011",
			}},
		{Key: "member_type", Label: "人员身份",
			Description: "同一个人可能先是学生后是教工，这里记录当前身份",
			Kind:        service.FieldKindSelect,
			Allowed:     []string{"FACULTY", "STUDENT", "STAFF", "VISITOR"},
			FilledFor: map[string]string{
				"shangguanrui": "FACULTY", "qinshuang": "FACULTY",
				"s2024010": "STUDENT", "s2023077": "STUDENT",
				"louzhen": "STAFF", "visiting.scholar": "VISITOR",
			}},
		{Key: "enrolled_on", Label: "入学/入职日期",
			Description: "用于计算学籍年限与账号回收时间",
			Kind:        service.FieldKindDate,
			FilledFor: map[string]string{
				"s2024010": "2024-09-01", "s2023077": "2023-09-01",
				"shangguanrui": "2018-07-15",
			}},
	},
	OAuth: []OAuthApp{
		{ClientID: "research-portal", Name: "科研管理平台", Type: model.AppTypeWeb,
			Redirect: []string{"https://research.edu.example.com/oauth2/callback"},
			Scopes:   []string{"openid", "profile", "email"},
			Launch:   "https://research.edu.example.com/"},
	},
	SAML: []SAMLApp{
		{Name: "Moodle 课程平台（SAML）", EntityID: "https://moodle.edu.example.com/saml/metadata",
			Host: "moodle.edu.example.com", Launch: "https://moodle.edu.example.com/"},
	},
	CAS: []CASApp{
		{Name: "教务系统（CAS）", Prefix: "https://jw.edu.example.com/",
			Launch: "https://jw.edu.example.com/"},
		{Name: "图书馆（CAS）", Prefix: "https://lib.edu.example.com/",
			Launch: "https://lib.edu.example.com/"},
	},
	Manager:  Assignment{Org: "cs", Username: "shangguanrui"},
	OrgAdmin: Assignment{Org: "registry", Username: "louzhen", Scope: model.OrgScopeSelf},
}
