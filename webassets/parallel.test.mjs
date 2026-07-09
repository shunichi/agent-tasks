// 並列稼働ビュー (parallel.js) の純粋ロジックの単体テスト。
//
// parallel.js は <script> にインライン展開されるため ESM の import/export を持てず、CommonJS の
// 存在ガード (module.exports) で純粋関数を公開している。ここではそれを Node ネイティブの require
// (createRequire) で読み込む。Vite 変換を介さないので module.exports がそのまま効き、document/window
// の無い Node では描画コード (startParallel) は走らず純粋関数だけが得られる。
import { describe, it, expect } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const P = require("./parallel.js");

describe("hm (分 → h/m 表記)", () => {
  it("60分未満は m", () => {
    expect(P.hm(0)).toBe("0m");
    expect(P.hm(1)).toBe("1m");
    expect(P.hm(59)).toBe("59m");
  });
  it("端数の四捨五入", () => {
    expect(P.hm(59.4)).toBe("59m");
    expect(P.hm(59.6)).toBe("1h"); // 60 に丸まる
  });
  it("時のみ / 時+分", () => {
    expect(P.hm(60)).toBe("1h");
    expect(P.hm(90)).toBe("1h30m");
    expect(P.hm(125)).toBe("2h5m");
  });
});

describe("fmtT (分 → HH:MM ゼロ詰め)", () => {
  it("ゼロ詰め", () => {
    expect(P.fmtT(0)).toBe("00:00");
    expect(P.fmtT(9 * 60 + 5)).toBe("09:05");
    expect(P.fmtT(23 * 60 + 59)).toBe("23:59");
  });
});

describe("pad2", () => {
  it("2桁ゼロ詰め", () => {
    expect(P.pad2(0)).toBe("00");
    expect(P.pad2(7)).toBe("07");
    expect(P.pad2(42)).toBe("42");
  });
});

describe("parseD / dowOf / dlabel (日付)", () => {
  it("parseD はローカル日付を返す", () => {
    const d = P.parseD("2026-07-09");
    expect(d.getFullYear()).toBe(2026);
    expect(d.getMonth()).toBe(6); // 0-indexed → 7月
    expect(d.getDate()).toBe(9);
  });
  it("dowOf は月曜=0 (0..6)", () => {
    expect(P.dowOf("2026-07-06")).toBe(0); // 月
    expect(P.dowOf("2026-07-07")).toBe(1); // 火
    expect(P.dowOf("2026-07-11")).toBe(5); // 土
    expect(P.dowOf("2026-07-12")).toBe(6); // 日
  });
  it("dlabel は M/D (曜) 形式", () => {
    expect(P.dlabel("2026-07-06")).toBe("7/6 (月)");
    expect(P.dlabel("2026-12-25")).toBe("12/25 (金)");
  });
});

describe("concAt (指定分の並列度)", () => {
  const list = [
    { s: 0, e: 60 },   // 00:00–01:00
    { s: 30, e: 90 },  // 00:30–01:30
    { s: 120, e: 180 } // 02:00–03:00
  ];
  it("開始は含み終了は含まない ([s, e))", () => {
    expect(P.concAt(list, 0)).toBe(1);   // 1本目のみ
    expect(P.concAt(list, 30)).toBe(2);  // 1本目+2本目
    expect(P.concAt(list, 60)).toBe(1);  // 1本目終了(含まず)、2本目のみ
    expect(P.concAt(list, 90)).toBe(0);  // 2本目終了(含まず)
    expect(P.concAt(list, 150)).toBe(1); // 3本目
    expect(P.concAt(list, 200)).toBe(0);
  });
  it("空リストは 0", () => {
    expect(P.concAt([], 100)).toBe(0);
  });
});

describe("byDate / datesDesc (日ごと集約)", () => {
  const pieces = [
    { d: "2026-07-06", id: "1", s: 0, e: 10 },
    { d: "2026-07-08", id: "2", s: 0, e: 10 },
    { d: "2026-07-06", id: "3", s: 20, e: 30 }
  ];
  it("byDate は日付キーでまとめる", () => {
    const m = P.byDate(pieces);
    expect(Object.keys(m).sort()).toEqual(["2026-07-06", "2026-07-08"]);
    expect(m["2026-07-06"]).toHaveLength(2);
    expect(m["2026-07-08"]).toHaveLength(1);
  });
  it("datesDesc は新しい順", () => {
    const m = P.byDate(pieces);
    expect(P.datesDesc(m)).toEqual(["2026-07-08", "2026-07-06"]);
  });
});

describe("mostActive (最もタスク種類の多い日)", () => {
  it("ユニーク id 数が最大の日を返す", () => {
    const pieces = [
      { d: "2026-07-06", id: "a", s: 0, e: 10 },
      { d: "2026-07-06", id: "a", s: 20, e: 30 }, // 同じ id → 1種類
      { d: "2026-07-08", id: "x", s: 0, e: 10 },
      { d: "2026-07-08", id: "y", s: 0, e: 10 }   // 2種類
    ];
    const m = P.byDate(pieces);
    const dates = P.datesDesc(m);
    expect(P.mostActive(m, dates)).toBe("2026-07-08");
  });
});

describe("esc / clip (文字列)", () => {
  it("esc は HTML 特殊文字を実体参照へ", () => {
    expect(P.esc('a<b>&"c')).toBe("a&lt;b&gt;&amp;&quot;c");
  });
  it("clip は n 超で … 付き短縮", () => {
    expect(P.clip("abc", 5)).toBe("abc");
    expect(P.clip("abcdef", 5)).toBe("abcd…");
  });
});
