#!/usr/bin/env python3
"""Ghi file .xlsx chi bang thu vien chuan (zipfile + XML).

Ly do khong dung openpyxl: python3 mac dinh tren may khong cai duoc package moi
(PEP 668 externally-managed). Skill phai chay duoc voi `python3` tran, khong
phu thuoc interpreter cu the hay virtualenv.

Ho tro dung du cho bao cao: nhieu sheet, so/chu, dinh dang (font/fill/border/
number format), do rong cot, merge cell, freeze pane, autofilter.

API:
    wb = Workbook()
    sh = wb.sheet("Tong quan")
    sh.widths({1: 26, 2: 12})
    sh.row([("Tieu de", "title")])
    sh.merge("A1:E1")
    sh.row(["a", 1.5, ("x", "num")])       # item = value hoac (value, style)
    sh.freeze(row=5)
    sh.autofilter("A5:E20")
    wb.save("out.xlsx")
"""
import zipfile

# ------------------------------------------------------------------ styles
# numFmtId tuy chinh bat dau tu 164.
NUM_FORMATS = {
    164: '0.##',                        # ngay cong
    165: '0.0"%"',                      # phan tram (luu 37.4)
    166: '"+"0.0"%";"-"0.0"%"',         # phan tram co dau
    167: '0.00"×"',                # he so nhan
    168: '0',                           # so nguyen
}

# (name, size, bold, italic, color)
FONTS = [
    ("Calibri", 11, False, False, "FF000000"),   # 0 default
    ("Calibri", 11, True, False, "FF000000"),    # 1 bold
    ("Calibri", 11, True, False, "FFFFFFFF"),    # 2 bold trang (header)
    ("Calibri", 16, True, False, "FF1F3864"),    # 3 title
    ("Calibri", 9, False, True, "FF595959"),     # 4 meta / note
    ("Calibri", 12, True, False, "FF1F3864"),    # 5 section
    ("Calibri", 11, True, False, "FFC00000"),    # 6 do dam (canh bao)
    ("Calibri", 10, False, False, "FF000000"),   # 7 nho
    ("Calibri", 10, False, True, "FF595959"),    # 8 nho in nghieng
]

FILLS = [
    None,          # 0 none
    "gray125",     # 1 bat buoc
    "FF1F3864",    # 2 header navy
    "FFD9E2F3",    # 3 xanh nhat (tong / section)
    "FFF2F2F2",    # 4 xam nhat (nhan KPI)
    "FFFFC7CE",    # 5 do nhat (Critical)
    "FFFFE699",    # 6 cam nhat (Major)
    "FFFFF2CC",    # 7 vang nhat (Minor)
    "FFE2EFDA",    # 8 xanh la nhat (tot)
    "FFFDEAEA",    # 9 hong nhat (khoi canh bao)
]

# xf: (numFmtId, fontId, fillId, borderId, halign, valign, wrap)
XFS = {
    "default":   (0, 0, 0, 0, None, None, False),
    "title":     (0, 3, 0, 0, "left", "center", False),
    "meta":      (0, 4, 0, 0, "left", "center", False),
    "note":      (0, 8, 0, 0, "left", "top", True),
    "warn":      (0, 6, 9, 1, "left", "top", True),
    "section":   (0, 5, 3, 0, "left", "center", False),

    "h":         (0, 2, 2, 1, "center", "center", True),
    "h_left":    (0, 2, 2, 1, "left", "center", True),

    "kpi_label": (0, 1, 4, 1, "left", "center", True),
    "kpi_num":   (164, 1, 0, 1, "right", "center", False),
    "kpi_pct":   (165, 1, 0, 1, "right", "center", False),
    "kpi_gain":  (166, 1, 0, 1, "right", "center", False),
    "kpi_x":     (167, 1, 0, 1, "right", "center", False),
    "kpi_text":  (0, 1, 0, 1, "right", "center", False),

    "cell":      (0, 0, 0, 1, "left", "center", False),
    "cell_wrap": (0, 0, 0, 1, "left", "top", True),
    "cell_c":    (0, 0, 0, 1, "center", "center", False),
    "int":       (168, 0, 0, 1, "right", "center", False),
    "num":       (164, 0, 0, 1, "right", "center", False),
    "pct":       (165, 0, 0, 1, "right", "center", False),
    "gain":      (166, 0, 0, 1, "right", "center", False),
    "mult":      (167, 0, 0, 1, "right", "center", False),

    "tot":       (0, 1, 3, 1, "left", "center", False),
    "tot_int":   (168, 1, 3, 1, "right", "center", False),
    "tot_num":   (164, 1, 3, 1, "right", "center", False),
    "tot_pct":   (165, 1, 3, 1, "right", "center", False),
    "tot_gain":  (166, 1, 3, 1, "right", "center", False),
    "tot_x":     (167, 1, 3, 1, "right", "center", False),

    "crit":      (0, 1, 5, 1, "center", "center", False),
    "major":     (0, 0, 6, 1, "center", "center", False),
    "minor":     (0, 0, 7, 1, "center", "center", False),
    "good":      (0, 0, 8, 1, "center", "center", False),

    "small":     (0, 7, 0, 1, "left", "center", False),
    "small_wrap": (0, 7, 0, 1, "left", "top", True),
    "small_num": (164, 7, 0, 1, "right", "center", False),
}
STYLE_IDS = {name: i for i, name in enumerate(XFS)}


def esc(text):
    return (str(text).replace("&", "&amp;").replace("<", "&lt;")
            .replace(">", "&gt;").replace('"', "&quot;"))


def col_letter(n):
    """1 -> A, 27 -> AA"""
    s = ""
    while n:
        n, r = divmod(n - 1, 26)
        s = chr(65 + r) + s
    return s


class Sheet:
    def __init__(self, name):
        self.name = name
        self._rows = []          # list[list[(col, value, style)]]
        self._widths = {}
        self._heights = {}
        self._merges = []
        self._freeze = None
        self._autofilter = None

    # -------------------------------------------------------------- noi dung
    def row(self, items=None, style="default", height=None):
        """items: list, moi phan tu la value hoac (value, style). Tra ve so dong."""
        cells = []
        for i, item in enumerate(items or [], start=1):
            value, st = item if isinstance(item, tuple) else (item, style)
            cells.append((i, value, st))
        self._rows.append(cells)
        r = len(self._rows)
        if height:
            self._heights[r] = height
        return r

    def blank(self, n=1):
        for _ in range(n):
            self._rows.append([])
        return len(self._rows)

    @property
    def cursor(self):
        """So dong hien tai (dong vua ghi)."""
        return len(self._rows)

    # ------------------------------------------------------------- dinh dang
    def widths(self, mapping):
        self._widths.update(mapping)

    def merge(self, ref):
        self._merges.append(ref)

    def freeze(self, row=0, col=0):
        self._freeze = (row, col)

    def autofilter(self, ref):
        self._autofilter = ref

    # ------------------------------------------------------------------- xml
    def xml(self):
        ncols = max((c[0] for r in self._rows for c in r), default=1)
        out = ['<?xml version="1.0" encoding="UTF-8" standalone="yes"?>',
               '<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">',
               f'<dimension ref="A1:{col_letter(ncols)}{max(len(self._rows), 1)}"/>']

        # sheetViews (pane phai nam trong sheetView)
        if self._freeze:
            fr, fc = self._freeze
            attrs = []
            if fc:
                attrs.append(f'xSplit="{fc}"')
            if fr:
                attrs.append(f'ySplit="{fr}"')
            top = f"{col_letter(fc + 1)}{fr + 1}"
            pane = ("bottomRight" if fr and fc else
                    "bottomLeft" if fr else "topRight")
            out.append('<sheetViews><sheetView workbookViewId="0">'
                       f'<pane {" ".join(attrs)} topLeftCell="{top}" '
                       f'activePane="{pane}" state="frozen"/>'
                       f'<selection pane="{pane}" activeCell="{top}" sqref="{top}"/>'
                       '</sheetView></sheetViews>')
        else:
            out.append('<sheetViews><sheetView workbookViewId="0"/></sheetViews>')

        out.append('<sheetFormatPr defaultRowHeight="15"/>')

        if self._widths:
            out.append("<cols>")
            for c, w in sorted(self._widths.items()):
                out.append(f'<col min="{c}" max="{c}" width="{w}" customWidth="1"/>')
            out.append("</cols>")

        out.append("<sheetData>")
        for ridx, cells in enumerate(self._rows, start=1):
            ht = self._heights.get(ridx)
            attr = f' ht="{ht}" customHeight="1"' if ht else ""
            if not cells:
                out.append(f'<row r="{ridx}"{attr}/>')
                continue
            out.append(f'<row r="{ridx}"{attr}>')
            for cidx, value, st in cells:
                ref = f"{col_letter(cidx)}{ridx}"
                s = STYLE_IDS.get(st, 0)
                if value is None or value == "":
                    out.append(f'<c r="{ref}" s="{s}"/>')
                elif isinstance(value, bool):
                    out.append(f'<c r="{ref}" s="{s}" t="inlineStr"><is><t>'
                               f'{"co" if value else "khong"}</t></is></c>')
                elif isinstance(value, (int, float)):
                    out.append(f'<c r="{ref}" s="{s}"><v>{value}</v></c>')
                else:
                    out.append(f'<c r="{ref}" s="{s}" t="inlineStr"><is>'
                               f'<t xml:space="preserve">{esc(value)}</t></is></c>')
            out.append("</row>")
        out.append("</sheetData>")

        # Thu tu bat buoc theo schema: autoFilter TRUOC mergeCells.
        if self._autofilter:
            out.append(f'<autoFilter ref="{self._autofilter}"/>')
        if self._merges:
            out.append(f'<mergeCells count="{len(self._merges)}">')
            for m in self._merges:
                out.append(f'<mergeCell ref="{m}"/>')
            out.append("</mergeCells>")

        out.append('<pageMargins left="0.5" right="0.5" top="0.6" bottom="0.6"'
                   ' header="0.3" footer="0.3"/>')
        out.append("</worksheet>")
        return "".join(out)


class Workbook:
    def __init__(self):
        self.sheets = []

    def sheet(self, name):
        # Excel: ten sheet toi da 31 ky tu, khong chua : \ / ? * [ ]
        safe = name[:31]
        for ch in ':\\/?*[]':
            safe = safe.replace(ch, "-")
        sh = Sheet(safe)
        self.sheets.append(sh)
        return sh

    # ------------------------------------------------------------- part xml
    def _styles(self):
        o = ['<?xml version="1.0" encoding="UTF-8" standalone="yes"?>',
             '<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">']
        o.append(f'<numFmts count="{len(NUM_FORMATS)}">')
        for fid, code in sorted(NUM_FORMATS.items()):
            o.append(f'<numFmt numFmtId="{fid}" formatCode="{esc(code)}"/>')
        o.append("</numFmts>")

        o.append(f'<fonts count="{len(FONTS)}">')
        for name, size, bold, italic, color in FONTS:
            o.append("<font>"
                     + ("<b/>" if bold else "")
                     + ("<i/>" if italic else "")
                     + f'<sz val="{size}"/><color rgb="{color}"/>'
                       f'<name val="{name}"/></font>')
        o.append("</fonts>")

        o.append(f'<fills count="{len(FILLS)}">')
        for f in FILLS:
            if f is None:
                o.append('<fill><patternFill patternType="none"/></fill>')
            elif f == "gray125":
                o.append('<fill><patternFill patternType="gray125"/></fill>')
            else:
                o.append('<fill><patternFill patternType="solid">'
                         f'<fgColor rgb="{f}"/><bgColor indexed="64"/>'
                         "</patternFill></fill>")
        o.append("</fills>")

        thin = '<left style="thin"><color rgb="FFBFBFBF"/></left>' \
               '<right style="thin"><color rgb="FFBFBFBF"/></right>' \
               '<top style="thin"><color rgb="FFBFBFBF"/></top>' \
               '<bottom style="thin"><color rgb="FFBFBFBF"/></bottom>'
        o.append('<borders count="2">'
                 "<border><left/><right/><top/><bottom/><diagonal/></border>"
                 f"<border>{thin}<diagonal/></border>"
                 "</borders>")

        o.append('<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0"'
                 ' borderId="0"/></cellStyleXfs>')
        o.append(f'<cellXfs count="{len(XFS)}">')
        for numfmt, font, fill, border, halign, valign, wrap in XFS.values():
            xf = (f'<xf numFmtId="{numfmt}" fontId="{font}" fillId="{fill}"'
                  f' borderId="{border}" xfId="0"'
                  f' applyNumberFormat="{1 if numfmt else 0}" applyFont="1"'
                  f' applyFill="{1 if fill else 0}"'
                  f' applyBorder="{1 if border else 0}"')
            if halign or valign or wrap:
                a = []
                if halign:
                    a.append(f'horizontal="{halign}"')
                if valign:
                    a.append(f'vertical="{valign}"')
                if wrap:
                    a.append('wrapText="1"')
                xf += f' applyAlignment="1"><alignment {" ".join(a)}/></xf>'
            else:
                xf += "/>"
            o.append(xf)
        o.append("</cellXfs>")
        o.append('<cellStyles count="1"><cellStyle name="Normal" xfId="0"'
                 ' builtinId="0"/></cellStyles>')
        o.append("</styleSheet>")
        return "".join(o)

    def save(self, path):
        n = len(self.sheets)
        ct = ['<?xml version="1.0" encoding="UTF-8" standalone="yes"?>',
              '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">',
              '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>',
              '<Default Extension="xml" ContentType="application/xml"/>',
              '<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>',
              '<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>']
        for i in range(1, n + 1):
            ct.append(f'<Override PartName="/xl/worksheets/sheet{i}.xml"'
                      ' ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>')
        ct.append("</Types>")

        wb = ['<?xml version="1.0" encoding="UTF-8" standalone="yes"?>',
              '<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"'
              ' xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">',
              "<sheets>"]
        for i, sh in enumerate(self.sheets, start=1):
            wb.append(f'<sheet name="{esc(sh.name)}" sheetId="{i}" r:id="rId{i}"/>')
        wb.append("</sheets></workbook>")

        rels = ['<?xml version="1.0" encoding="UTF-8" standalone="yes"?>',
                '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">']
        for i in range(1, n + 1):
            rels.append(f'<Relationship Id="rId{i}"'
                        ' Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"'
                        f' Target="worksheets/sheet{i}.xml"/>')
        rels.append(f'<Relationship Id="rId{n + 1}"'
                    ' Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"'
                    ' Target="styles.xml"/>')
        rels.append("</Relationships>")

        root_rels = ('<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
                     '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
                     '<Relationship Id="rId1"'
                     ' Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"'
                     ' Target="xl/workbook.xml"/></Relationships>')

        with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
            z.writestr("[Content_Types].xml", "".join(ct))
            z.writestr("_rels/.rels", root_rels)
            z.writestr("xl/workbook.xml", "".join(wb))
            z.writestr("xl/_rels/workbook.xml.rels", "".join(rels))
            z.writestr("xl/styles.xml", self._styles())
            for i, sh in enumerate(self.sheets, start=1):
                z.writestr(f"xl/worksheets/sheet{i}.xml", sh.xml())
        return path
