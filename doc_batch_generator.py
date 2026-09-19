# -*- coding: utf-8 -*-
"""
文档批量生成脚本（热拔插脚本）

功能：
- 从三种数据来源读取 Excel 数据表：
  1. 中控台数据表格（默认）：读取平台 online_config / 本地 Excel 路径
  2. 本地 Excel 文件：选择单个 .xlsx/.xlsm 文件
  3. 金山文档在线表格：用户自行配置 Webhook 链接和令牌
- 指定标题所在行，据此识别列标题
- 两种数据行选定方式（均可新增多条规则、拖拽排序、按顺序执行）：
  · 内容模式：选择「标题列 + 该列下的内容值」→ 选中所有匹配的数据行
  · 数字模式：输入「第 x 行至第 y 行」→ 选中指定行范围内的数据行
- 每条规则单独配置模板（模板所在文件夹 + 选择方式）：
  · 当前所有模板：勾选文件夹下模板文件（全选/全不选/反选）
  · 分组模板：新建/删除/重命名分组，按分组调用模板（分组持久化在前端 localStorage）
- 模板中【列名】占位符按数据行内容替换；【列名|日期格式】支持格式化；
  未匹配到的占位符原样保留
- 生成后文件命名：从标题列名称 + 「模板名称」中多选并拖拽排序后叠加命名
  （遵守 Windows 文件名长度限制，超长则该文件失败并报原因）
- 执行后输出文件并显示执行日志与 5 秒汇总提示

依赖：openpyxl（xlsx 模板读写）、python-docx（docx 模板，可选）、requests（金山文档）
"""

import os
import re
import shutil
import datetime
from typing import Any, Dict, List, Optional, Tuple

# ============ 脚本元信息 ============
TOOL_NAME = "文档批量生成"
TOOL_DESC = "借助 Excel 数据表与模板，按规则批量生成文件资料"
SCRIPT_VERSION = "1.1.0"
CATEGORY = "单独功能"
AUTHOR = ""
UPDATE_DATE = "2026-07-30"

# AI 调用提示（用于 CLI/MCP Server 自描述）
PARAMS_HINT = "data_source=dashboard|local|online header_row=标题行号 output_folder=输出目录 create_subfolder=true|false"
DESTRUCTIVE = False  # 仅生成新文件，不修改原文件

UI_SCHEMA = [
    # ---------- 1. 数据来源选择 ----------
    {
        "key": "data_source",
        "label": "数据来源",
        "type": "select",
        "required": True,
        "default": "dashboard",
        "options": [
            {"value": "dashboard", "label": "中控台数据表格（默认）"},
            {"value": "local", "label": "本地 Excel 数据表格"},
            {"value": "online", "label": "金山文档在线表格"},
        ],
        "help": "中控台模式：自动读取平台已配置的金山文档 / 本地 Excel；"
                "本地模式：选择单个 Excel 文件；金山模式：使用下方填写的链接和令牌",
    },
    {
        "key": "local_file",
        "label": "本地 Excel 数据表格",
        "type": "file_picker",
        "file_filter": "excel",
        "required": False,
        "placeholder": "点击右侧按钮选择 Excel 文件",
        "help": "选择单个 .xlsx/.xlsm 文件作为数据源",
        "visible_when": {"field": "data_source", "value": "local"},
    },
    {
        "key": "online_webhook_url",
        "label": "金山文档 Webhook 链接",
        "type": "text",
        "required": False,
        "placeholder": "https://www.kdocs.cn/... 或 AirScript webhook 地址",
        "help": "金山文档在线表格的同步 webhook 地址",
        "visible_when": {"field": "data_source", "value": "online"},
    },
    {
        "key": "online_api_token",
        "label": "金山文档脚本令牌",
        "type": "text",
        "required": False,
        "placeholder": "AirScript Token",
        "help": "在金山文档 AirScript 编辑器中生成的 API Token",
        "visible_when": {"field": "data_source", "value": "online"},
    },

    # ---------- 2. 标题所在行 ----------
    {
        "key": "header_row",
        "label": "标题所在行",
        "type": "number",
        "default": 1,
        "required": True,
        "placeholder": "如 3 代表标题行在第 3 行",
        "help": "数字是几，标题就在第几行（1 代表第 1 行）",
    },

    # ---------- 3. 数据行规则 + 模板选择 + 命名（自定义复合字段） ----------
    {
        "key": "batch_config",
        "label": "数据行规则与模板配置",
        "type": "batch_doc_config",
        "fetch_source": "excel_sheet_names",
        "depends_on": ["data_source", "local_file",
                       "online_webhook_url", "online_api_token", "header_row"],
        "required": True,
        "placeholder": "请先完成数据来源与标题行配置",
        "help": "选择 Sheet → 新增数据行规则（内容/数字两种模式）→ 每条规则配置模板 → 配置命名方式",
    },

    # ---------- 4. 输出位置 ----------
    {
        "key": "output_folder",
        "label": "生成文件存储位置",
        "type": "folder_picker",
        "required": True,
        "placeholder": "点击右侧按钮选择存储位置",
        "help": "批量生成的文件将保存到此文件夹",
    },

    # ---------- 5. 是否新建文件夹 ----------
    {
        "key": "create_subfolder",
        "label": "是否新建文件夹",
        "type": "select",
        "required": True,
        "default": "no",
        "options": [
            {"value": "no", "label": "否（直接生成至存放位置）"},
            {"value": "yes", "label": "是（按命名项建立文件夹）"},
        ],
        "help": '选择"否"：直接将文件生成至存放位置；'
                '选择"是"：先按命名项（不含模板名）建立文件夹，再将文件生成至对应文件夹',
    },
]
# ====================================


# ============================================================
# 通用工具
# ============================================================

def _fail(msg: str, logs: List[str] = None) -> dict:
    result = {"success": False, "message": msg, "data": None}
    if logs:
        result["logs"] = logs
    return result


def _cell_str(val: Any) -> str:
    """将单元格值转为字符串"""
    if val is None:
        return ""
    if isinstance(val, str):
        return val.strip()
    if isinstance(val, float):
        if val.is_integer():
            return str(int(val))
        return str(val)
    if isinstance(val, datetime.datetime):
        return val.strftime("%Y-%m-%d %H:%M:%S")
    if isinstance(val, datetime.date):
        return val.strftime("%Y-%m-%d")
    return str(val).strip()


def _is_empty_row(row: List[Any]) -> bool:
    return not row or all(v is None or (isinstance(v, str) and not v.strip())
                         for v in row)


def _find_last_data_row(rows: List[List[Any]]) -> int:
    last = -1
    for i in range(len(rows) - 1, -1, -1):
        if not _is_empty_row(rows[i]):
            last = i
            break
    return last


# ============================================================
# 数据源加载
# ============================================================

def _load_local_sheet(file_path: str, sheet_name: str, logs: List[str]):
    try:
        import openpyxl
    except ImportError:
        return None, "未安装 openpyxl"
    try:
        wb = openpyxl.load_workbook(file_path, data_only=True, read_only=True)
        if sheet_name not in wb.sheetnames:
            wb.close()
            return None, "Sheet「{}」不存在".format(sheet_name)
        ws = wb[sheet_name]
        rows = [list(r) for r in ws.iter_rows(values_only=True)]
        wb.close()
        logs.append("[数据源] 本地文件：{} / {}".format(
            os.path.basename(file_path), sheet_name))
        return rows, None
    except Exception as e:
        return None, "读取本地文件失败：{}".format(str(e))


def _load_online_sheet(webhook_url: str, api_token: str,
                       sheet_name: str, logs: List[str]):
    try:
        from src.online_source import OnlineAirScriptSource
    except ImportError:
        return None, "无法导入 OnlineAirScriptSource，请确认脚本在平台内运行"
    src = OnlineAirScriptSource(webhook_url, api_token)
    r = src.test_connection()
    if not r.get("success"):
        return None, "连通测试失败：{}".format(r.get("message", "未知错误"))
    r = src.fetch_all_data()
    if not r.get("success"):
        return None, "拉取数据失败：{}".format(r.get("message", "未知错误"))
    data = src.get_sheet_data(sheet_name)
    if data is None:
        return None, "在线表格中不存在 Sheet「{}」".format(sheet_name)
    logs.append("[数据源] 金山文档：{} / {}".format(
        webhook_url[:40] + "...", sheet_name))
    return data, None


def _load_dashboard_sheet(sheet_name: str, logs: List[str]):
    """中控台模式：优先用 online_config，回退到本地 Excel 路径"""
    try:
        from src.platform_api import PlatformAPI
    except ImportError:
        return None, "无法导入 PlatformAPI，请确认脚本在平台内运行"

    # 1) 在线配置
    online_cfg = PlatformAPI.get_config("online_config", {})
    if isinstance(online_cfg, dict):
        webhook = (online_cfg.get("webhook_url") or "").strip()
        token = (online_cfg.get("api_token") or "").strip()
        if webhook and token:
            return _load_online_sheet(webhook, token, sheet_name, logs)

    # 2) 本地 Excel 路径
    ed = PlatformAPI.get_excel_data()
    if ed and ed.get("excel_path"):
        path = ed["excel_path"]
        if os.path.isfile(path):
            logs.append("[数据源] 中控台本地 Excel：{}".format(path))
            return _load_local_sheet(path, sheet_name, logs)

    return None, ("中控台数据源未就绪：未配置金山文档链接/令牌，"
                  "也未配置本地 Excel 路径")


def _load_sheet_rows(data_source: str, params: dict, sheet_name: str,
                     logs: List[str]) -> Tuple[Optional[List[List[Any]]], Optional[str]]:
    if data_source == "dashboard":
        return _load_dashboard_sheet(sheet_name, logs)
    if data_source == "local":
        local_file = (params.get("local_file") or "").strip()
        if not local_file or not os.path.isfile(local_file):
            return None, "请选择有效的本地 Excel 文件"
        return _load_local_sheet(local_file, sheet_name, logs)
    if data_source == "online":
        webhook = (params.get("online_webhook_url") or "").strip()
        token = (params.get("online_api_token") or "").strip()
        if not webhook or not token:
            return None, "请填写金山文档 Webhook 链接和令牌"
        return _load_online_sheet(webhook, token, sheet_name, logs)
    return None, "未知数据源：{}".format(data_source)


# ============================================================
# 占位符引擎
# ============================================================

_PLACEHOLDER_RE = re.compile(r"【([^【】]+)】")
_DATE_FORMAT_TOKENS = [
    ("YYYY", "%Y"), ("YY", "%y"),
    ("MM", "%m"), ("DD", "%d"),
    ("HH", "%H"), ("mm", "%M"), ("ss", "%S"),
]


def _parse_date(val: Any) -> Optional[datetime.datetime]:
    if isinstance(val, datetime.datetime):
        return val
    if isinstance(val, datetime.date):
        return datetime.datetime(val.year, val.month, val.day)
    if val is None:
        return None
    s = str(val).strip()
    if not s:
        return None
    # 去掉可能的时秒分只保留日期部分尝试
    fmts = [
        "%Y-%m-%d", "%Y/%m/%d", "%Y.%m.%d", "%Y%m%d",
        "%Y-%m-%d %H:%M:%S", "%Y/%m/%d %H:%M:%S",
        "%Y年%m月%d日", "%Y-%m", "%Y/%m",
    ]
    for f in fmts:
        try:
            return datetime.datetime.strptime(s, f)
        except ValueError:
            continue
    return None


def _apply_format(val: Any, fmt: str) -> str:
    """按日期格式化；无法解析则返回原始字符串"""
    if not fmt:
        return _cell_str(val)
    dt = _parse_date(val)
    if dt is None:
        return _cell_str(val)
    py_fmt = fmt
    for token, repl in _DATE_FORMAT_TOKENS:
        py_fmt = py_fmt.replace(token, repl)
    try:
        return dt.strftime(py_fmt)
    except Exception:
        return _cell_str(val)


def _render_text(text: str, fields: Dict[str, str]) -> Tuple[str, List[str]]:
    """替换文本中的 【列名】 / 【列名|格式】 占位符

    Returns:
        (替换后文本, 未匹配占位符名称列表)
    """
    if not isinstance(text, str) or "【" not in text:
        return text, []
    unmatched: List[str] = []

    def _repl(m):
        expr = m.group(1).strip()
        if "|" in expr:
            name, fmt = expr.split("|", 1)
            name = name.strip()
            fmt = fmt.strip()
        else:
            name, fmt = expr, ""
        if name in fields:
            return _apply_format(fields[name], fmt)
        unmatched.append(name)
        return m.group(0)  # 原样保留

    new_text = _PLACEHOLDER_RE.sub(_repl, text)
    return new_text, unmatched


# ============================================================
# 模板生成器
# ============================================================

def _generate_xlsx(template_path: str, output_path: str,
                   fields: Dict[str, str], logs: List[str]) -> Tuple[bool, str]:
    try:
        import openpyxl
    except ImportError:
        return False, "未安装 openpyxl"
    try:
        wb = openpyxl.load_workbook(template_path)
    except Exception as e:
        return False, "加载模板失败：{}".format(str(e))
    unmatched_all: List[str] = []
    try:
        for ws in wb.worksheets:
            for row in ws.iter_rows():
                for cell in row:
                    if isinstance(cell.value, str) and "【" in cell.value:
                        new_val, unmatched = _render_text(cell.value, fields)
                        cell.value = new_val
                        unmatched_all.extend(unmatched)
        wb.save(output_path)
    except Exception as e:
        return False, "保存失败：{}".format(str(e))
    finally:
        wb.close()
    msg = "已生成"
    if unmatched_all:
        msg += "（未匹配占位符：{}）".format(
            "、".join(sorted(set(unmatched_all))[:5]))
    return True, msg


def _generate_docx(template_path: str, output_path: str,
                   fields: Dict[str, str], logs: List[str]) -> Tuple[bool, str]:
    try:
        import docx
    except ImportError:
        return False, "未安装 python-docx，无法生成 docx"
    try:
        doc = docx.Document(template_path)
    except Exception as e:
        return False, "加载模板失败：{}".format(str(e))

    unmatched_all: List[str] = []

    def _render_paragraphs(paragraphs):
        for p in paragraphs:
            if not p.runs:
                # 无 run 的段落直接替换 text
                if p.text and "【" in p.text:
                    new_text, unmatched = _render_text(p.text, fields)
                    p.text = new_text
                    unmatched_all.extend(unmatched)
                continue
            full = "".join(r.text for r in p.runs)
            if "【" in full:
                new_text, unmatched = _render_text(full, fields)
                # 写回首个 run，清空其余 run（保留首 run 格式）
                p.runs[0].text = new_text
                for r in p.runs[1:]:
                    r.text = ""
                unmatched_all.extend(unmatched)

    _render_paragraphs(doc.paragraphs)
    # 表格内段落
    for table in doc.tables:
        for row in table.rows:
            for cell in row.cells:
                _render_paragraphs(cell.paragraphs)

    try:
        doc.save(output_path)
    except Exception as e:
        return False, "保存失败：{}".format(str(e))
    msg = "已生成"
    if unmatched_all:
        msg += "（未匹配占位符：{}）".format(
            "、".join(sorted(set(unmatched_all))[:5]))
    return True, msg


def _generate_text_like(template_path: str, output_path: str,
                        fields: Dict[str, str], logs: List[str]) -> Tuple[bool, str]:
    """csv / txt 等纯文本模板：整体替换"""
    try:
        with open(template_path, "r", encoding="utf-8") as f:
            content = f.read()
    except Exception:
        try:
            with open(template_path, "r", encoding="gbk") as f:
                content = f.read()
        except Exception as e:
            return False, "读取模板失败：{}".format(str(e))
    new_content, unmatched = _render_text(content, fields)
    try:
        with open(output_path, "w", encoding="utf-8") as f:
            f.write(new_content)
    except Exception as e:
        return False, "保存失败：{}".format(str(e))
    msg = "已生成"
    if unmatched:
        msg += "（未匹配占位符：{}）".format("、".join(sorted(set(unmatched))[:5]))
    return True, msg


def _generate_file(template_path: str, output_path: str,
                   fields: Dict[str, str], logs: List[str]) -> Tuple[bool, str]:
    ext = os.path.splitext(template_path)[1].lower()
    if ext in (".xlsx", ".xlsm"):
        return _generate_xlsx(template_path, output_path, fields, logs)
    if ext in (".docx",):
        return _generate_docx(template_path, output_path, fields, logs)
    if ext in (".csv", ".txt"):
        return _generate_text_like(template_path, output_path, fields, logs)
    return False, "不支持的模板类型：{}（支持 .xlsx/.xlsm/.docx/.csv/.txt）".format(ext)


# ============================================================
# 命名与路径
# ============================================================

_INVALID_NAME_CHARS = '<>:"/\\|?*'


def _sanitize_filename(name: str) -> str:
    """清理 Windows 文件名非法字符"""
    for ch in _INVALID_NAME_CHARS:
        name = name.replace(ch, "_")
    name = name.strip().rstrip(".")
    # 折叠连续空白
    name = re.sub(r"\s+", " ", name)
    return name


def _template_display_name(filename: str) -> str:
    """从模板文件名提取「名称」部分

    规则：去扩展名 → 去掉开头的序号前缀（如 "1." "10、"）→
          去掉开头的「模板-」/「模版-」前缀
    例： "1.模板-开工报告.xlsx" → "开工报告"
    """
    name = os.path.splitext(filename)[0]
    name = re.sub(r"^\s*\d+\s*[.、\-]\s*", "", name)
    name = re.sub(r"^模[板版]\s*[-_、]\s*", "", name)
    return name.strip() or os.path.splitext(filename)[0]


def _build_filename(naming_items: List[dict], fields: Dict[str, str],
                    template_filename: str) -> Tuple[str, Optional[str]]:
    """根据命名项构建文件名（不含扩展名）

    Returns:
        (文件名主体, 错误原因)  错误原因为 None 表示正常
    """
    parts: List[str] = []
    for item in naming_items:
        itype = item.get("type", "column")
        key = item.get("key", "")
        if itype == "template":
            parts.append(_template_display_name(template_filename))
        else:
            val = fields.get(key, "")
            if not val:
                val = ""
            parts.append(str(val))
    name = "".join(parts)
    name = _sanitize_filename(name)
    if not name:
        return "", "命名结果为空（请检查命名选项对应列是否有值）"
    # Windows 文件名分量上限 255 字符
    if len(name) > 255:
        return "", "文件名过长（{} 字符，超过 255 上限），无法保持全名".format(len(name))
    return name, None


def _build_folder_name(naming_items: List[dict], fields: Dict[str, str]
                       ) -> Tuple[str, Optional[str]]:
    """根据命名项中的「列」类型项构建文件夹名（不含模板项）

    用于「是否新建文件夹=是」时，按当前数据行的命名项值建立子文件夹。
    例：命名项为 [项目名称(列), 模板名(模板)]，当前行项目名称为"项目A"，
        则文件夹名 = "项目A"，模板生成的文件放入该文件夹。

    Returns:
        (文件夹名, 错误原因)  错误原因为 None 表示正常
    """
    parts: List[str] = []
    for item in naming_items:
        itype = item.get("type", "column")
        key = item.get("key", "")
        if itype == "template":
            continue
        val = fields.get(key, "")
        if not val:
            val = ""
        parts.append(str(val))
    name = "".join(parts)
    name = _sanitize_filename(name)
    if not name:
        return "", "文件夹命名结果为空（请检查命名选项对应列是否有值或包含列类型命名项）"
    if len(name) > 255:
        return "", "文件夹名过长（{} 字符，超过 255 上限）".format(len(name))
    return name, None


def _resolve_output_path(folder: str, base_name: str, ext: str,
                         used_paths: set) -> str:
    """生成最终输出路径，遇重名自动加序号后缀"""
    candidate = os.path.join(folder, base_name + ext)
    if candidate not in used_paths and not os.path.exists(candidate):
        used_paths.add(candidate)
        return candidate
    n = 2
    while True:
        cand = os.path.join(folder, "{} ({}){}".format(base_name, n, ext))
        if cand not in used_paths and not os.path.exists(cand):
            used_paths.add(cand)
            return cand
        n += 1
        if n > 9999:
            used_paths.add(candidate)
            return candidate


# ============================================================
# 规则与数据行解析
# ============================================================

def _build_header_map(rows: List[List[Any]], header_row: int) -> Tuple[Dict[str, int], List[str]]:
    """构建 {列名: 列索引} 映射（重复列名取首个）"""
    if header_row < 1 or header_row > len(rows):
        return {}, []
    headers = rows[header_row - 1]
    col_map: Dict[str, int] = {}
    header_names: List[str] = []
    for i, h in enumerate(headers):
        if h is None:
            continue
        h_str = str(h).strip() if not isinstance(h, str) else h.strip()
        if not h_str:
            continue
        if h_str not in col_map:
            col_map[h_str] = i
            header_names.append(h_str)
    return col_map, header_names


def _row_to_fields(row: List[Any], col_map: Dict[str, int]) -> Dict[str, str]:
    fields: Dict[str, str] = {}
    for name, idx in col_map.items():
        if idx < len(row):
            fields[name] = _cell_str(row[idx])
        else:
            fields[name] = ""
    return fields


def _select_content_rows(rows: List[List[Any]], header_row: int,
                          col_map: Dict[str, int], title_column: str,
                          content_value: str) -> List[int]:
    """内容模式：返回所有满足 col[title_column] == content_value 的数据行索引（0-based）"""
    idx = col_map.get(title_column)
    if idx is None:
        return []
    result: List[int] = []
    target = (content_value or "").strip()
    for i in range(header_row, len(rows)):
        row = rows[i]
        if _is_empty_row(row):
            continue
        val = row[idx] if idx < len(row) else None
        val_str = _cell_str(val)
        if val_str == target:
            result.append(i)
    return result


def _select_number_rows(rows: List[List[Any]], row_start: int,
                         row_end: int) -> List[int]:
    """数字模式：返回 [row_start-1, row_end-1] 范围内非空数据行索引（0-based）"""
    start = max(1, row_start)
    end = min(len(rows), row_end)
    result: List[int] = []
    for i in range(start - 1, end):
        if not _is_empty_row(rows[i]):
            result.append(i)
    return result


def _resolve_templates(rule: dict, groups: Dict[str, List[str]],
                       default_dir: str, logs: List[str]) -> Tuple[List[str], Optional[str]]:
    """解析规则最终要用的模板文件名列表"""
    folder = (rule.get("template_folder") or "").strip() or default_dir
    mode = rule.get("template_mode", "all")
    names: List[str] = []
    if mode == "group":
        sel_groups = rule.get("selected_groups") or []
        for gname in sel_groups:
            names.extend(groups.get(gname, []))
        # 去重保序
        seen = set()
        unique = []
        for n in names:
            if n not in seen:
                seen.add(n)
                unique.append(n)
        names = unique
    else:
        names = list(rule.get("selected_templates") or [])

    if not names:
        return [], "未选择任何模板"

    # 校验文件存在
    missing = [n for n in names if not os.path.isfile(os.path.join(folder, n))]
    if missing:
        return names, "模板文件夹中找不到：{}".format("、".join(missing[:5]))
    return names, None


# ============================================================
# 主入口
# ============================================================

def run(params: dict) -> dict:
    """脚本主入口

    参数:
        params: dict - 用户填写的表单参数，包含 batch_config 复合配置

    返回:
        dict - {success, message, data:{total, success_count, failed_count, details, summary}, logs}
    """
    logs: List[str] = []
    logs.append("========================================")
    logs.append("文档批量生成脚本启动")
    logs.append("========================================")

    # ---------- 1. 参数校验 ----------
    data_source = (params.get("data_source") or "dashboard").strip()
    if data_source not in ("dashboard", "local", "online"):
        return _fail("数据来源无效：{}".format(data_source), logs)

    try:
        header_row = int(params.get("header_row", 1))
    except (ValueError, TypeError):
        return _fail("标题所在行必须是数字", logs)
    if header_row < 1:
        return _fail("标题所在行必须大于 0", logs)

    output_folder = (params.get("output_folder") or "").strip()
    if not output_folder:
        return _fail("请选择生成文件存储位置", logs)
    if not os.path.isdir(output_folder):
        return _fail("存储位置不存在或不是目录：{}".format(output_folder), logs)

    create_subfolder = (params.get("create_subfolder") or "no").strip()
    if create_subfolder not in ("yes", "no"):
        create_subfolder = "no"

    batch_config = params.get("batch_config")
    if not batch_config or not isinstance(batch_config, dict):
        return _fail("请完成数据行规则与模板配置", logs)

    sheet_name = (batch_config.get("sheet_name") or "").strip()
    if not sheet_name:
        return _fail("请在配置中选择数据 Sheet", logs)

    content_rules = batch_config.get("content_rules") or []
    number_rules = batch_config.get("number_rules") or []
    if not content_rules and not number_rules:
        return _fail("请至少新增一条数据行规则", logs)

    naming = batch_config.get("naming") or {}
    naming_items = naming.get("items") or []
    if not naming_items:
        return _fail("请至少选择一个命名选项", logs)

    groups = batch_config.get("groups") or {}
    if not isinstance(groups, dict):
        groups = {}

    # 默认模板目录
    default_tpl_dir = ""
    try:
        from src.platform_api import PlatformAPI
        default_tpl_dir = PlatformAPI.get_templates_dir() or ""
    except Exception:
        pass
    if not default_tpl_dir:
        # 回退：尝试相对路径
        try:
            from src.paths import get_templates_dir
            default_tpl_dir = get_templates_dir()
        except Exception:
            pass

    logs.append("[配置] 数据来源：{} | 标题行：第 {} 行 | Sheet：{}".format(
        data_source, header_row, sheet_name))
    logs.append("[配置] 内容规则 {} 条 | 数字规则 {} 条 | 命名项 {} 个".format(
        len(content_rules), len(number_rules), len(naming_items)))
    logs.append("[配置] 是否新建文件夹：{}".format("是（按命名项建立子文件夹）"
               if create_subfolder == "yes" else "否（直接生成至存放位置）"))

    # ---------- 2. 加载数据 ----------
    rows, err = _load_sheet_rows(data_source, params, sheet_name, logs)
    if err:
        return _fail(err, logs)
    if not rows:
        return _fail("Sheet「{}」无数据".format(sheet_name), logs)

    # 截断尾部空行
    last = _find_last_data_row(rows)
    if last >= 0:
        rows = rows[:last + 1]

    col_map, header_names = _build_header_map(rows, header_row)
    if not col_map:
        return _fail("标题行第 {} 行为空或全部为空单元格".format(header_row), logs)
    logs.append("[配置] 识别到 {} 个列标题：{}".format(
        len(header_names), "、".join(header_names[:8]) +
        ("..." if len(header_names) > 8 else "")))

    # ---------- 3. 按规则执行 ----------
    details: List[List[str]] = []  # [状态, 文件名, 说明]
    total_gen = 0
    success_count = 0
    failed_count = 0
    used_paths: set = set()
    created_subfolders: set = set()  # 已创建的子文件夹（避免重复日志）

    def _process_rule(rule: dict, mode_label: str):
        nonlocal total_gen, success_count, failed_count

        # 选数据行
        if rule.get("mode") == "number":
            try:
                rs = int(rule.get("row_start", 0))
            except (ValueError, TypeError):
                rs = 0
            try:
                re_ = int(rule.get("row_end", 0))
            except (ValueError, TypeError):
                re_ = 0
            if rs < 1 or re_ < rs:
                details.append(["failed", mode_label, "行号范围无效：{}-{}".format(rs, re_)])
                logs.append("[规则][{}] 行号范围无效，跳过".format(mode_label))
                return
            row_idxs = _select_number_rows(rows, rs, re_)
            logs.append("[规则][{}] 数字模式 第 {}-{} 行 → {} 行数据".format(
                mode_label, rs, re_, len(row_idxs)))
        else:
            tc = (rule.get("title_column") or "").strip()
            cv = (rule.get("content_value") or "").strip()
            if not tc:
                details.append(["failed", mode_label, "未选择标题列"])
                logs.append("[规则][{}] 未选择标题列，跳过".format(mode_label))
                return
            if not cv:
                details.append(["failed", mode_label, "未选择内容值"])
                logs.append("[规则][{}] 未选择内容值，跳过".format(mode_label))
                return
            row_idxs = _select_content_rows(rows, header_row, col_map, tc, cv)
            logs.append("[规则][{}] 内容模式「{}={}] → {} 行数据".format(
                mode_label, tc, cv, len(row_idxs)))

        if not row_idxs:
            details.append(["failed", mode_label, "未匹配到任何数据行"])
            logs.append("[规则][{}] 未匹配到数据行".format(mode_label))
            return

        # 解析模板
        tpl_names, terr = _resolve_templates(rule, groups, default_tpl_dir, logs)
        if terr:
            # 模板缺失视为整条规则失败
            details.append(["failed", mode_label, terr])
            logs.append("[规则][{}] {}".format(mode_label, terr))
            return
        logs.append("[规则][{}] 选用 {} 个模板：{}".format(
            mode_label, len(tpl_names), "、".join(tpl_names[:5]) +
            ("..." if len(tpl_names) > 5 else "")))

        # 逐行 × 逐模板生成
        for ridx in row_idxs:
            row = rows[ridx]
            fields = _row_to_fields(row, col_map)

            # 确定目标文件夹（按是否新建文件夹）
            target_folder = output_folder
            if create_subfolder == "yes":
                folder_name, ferr = _build_folder_name(naming_items, fields)
                if ferr:
                    # 文件夹命名失败：该行所有模板文件均计为失败
                    for tpl_name in tpl_names:
                        total_gen += 1
                        failed_count += 1
                        details.append(["failed", tpl_name,
                                        "第 {} 行：{}".format(ridx + 1, ferr)])
                    logs.append("[规则][{}] 第 {} 行：{}".format(
                        mode_label, ridx + 1, ferr))
                    continue
                target_folder = os.path.join(output_folder, folder_name)
                try:
                    os.makedirs(target_folder, exist_ok=True)
                except Exception as e:
                    for tpl_name in tpl_names:
                        total_gen += 1
                        failed_count += 1
                        details.append(["failed", tpl_name,
                                        "第 {} 行：创建文件夹失败：{}".format(
                                            ridx + 1, str(e))])
                    logs.append("[规则][{}] 第 {} 行：创建文件夹失败：{}".format(
                        mode_label, ridx + 1, str(e)))
                    continue
                if target_folder not in created_subfolders:
                    created_subfolders.add(target_folder)
                    logs.append("[文件夹] 新建：{}".format(folder_name))

            for tpl_name in tpl_names:
                total_gen += 1
                tpl_folder = (rule.get("template_folder") or "").strip() or default_tpl_dir
                tpl_path = os.path.join(tpl_folder, tpl_name)
                ext = os.path.splitext(tpl_name)[1]

                base_name, nerr = _build_filename(naming_items, fields, tpl_name)
                if nerr:
                    failed_count += 1
                    details.append(["failed", tpl_name,
                                    "第 {} 行：{}".format(ridx + 1, nerr)])
                    continue

                out_path = _resolve_output_path(target_folder, base_name, ext, used_paths)
                ok_flag, msg = _generate_file(tpl_path, out_path, fields, logs)
                if ok_flag:
                    success_count += 1
                    rel = os.path.relpath(out_path, output_folder)
                    details.append(["success", rel,
                                    "第 {} 行 · {}".format(ridx + 1, msg)])
                else:
                    failed_count += 1
                    details.append(["failed", tpl_name,
                                    "第 {} 行：{}".format(ridx + 1, msg)])

    # 内容规则先于数字规则执行（均按列表顺序）
    for i, rule in enumerate(content_rules):
        _process_rule(rule, "内容规则#{}".format(i + 1))
    for i, rule in enumerate(number_rules):
        _process_rule(rule, "数字规则#{}".format(i + 1))

    # ---------- 4. 汇总 ----------
    subfolder_info = ""
    if create_subfolder == "yes" and created_subfolders:
        subfolder_info = "，新建文件夹 {} 个".format(len(created_subfolders))
    summary = "生成完成：共 {} 个文件，成功 {}，失败 {}{}{}".format(
        total_gen, success_count, failed_count, subfolder_info,
        "（输出：{}）".format(output_folder) if output_folder else "")
    logs.append("[完成] {}".format(summary))

    return {
        "success": failed_count == 0 and success_count > 0,
        "message": summary,
        "data": {
            "total": total_gen,
            "success_count": success_count,
            "failed_count": failed_count,
            "details": details,
            "summary": summary,
        },
        "logs": logs,
    }
