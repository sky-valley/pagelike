// Documents the acceptance suite builds from the docs snapshot
// (research/docs/2026-09-28/md), exactly as printed, per docs/spec/apps.md §6.3.
import { docPage, codeBlocks } from "./apps.mjs";

/** A1: the tutorial's final document (learn/your-first-app lines 238-320)
 * with its inline script replaced by the DOMSubscriber version (329-359). */
export function firstAppDocument() {
  const blocks = codeBlocks(docPage("learn_your-first-app"));
  const full = blocks.filter((b) => b.code.startsWith("<!doctype html>") && b.code.includes("button.delete {")).pop().code;
  const sub = blocks.find((b) => b.code.includes("DOMSubscriber.subscribe")).code;
  const inline = /( *)<script type="module">\n[\s\S]*?<\/script>/;
  const indent = inline.exec(full)[1];
  return full.replace(inline, () => sub.split("\n").map((l) => indent + l).join("\n")) + "\n";
}

/** A1: the same document without its two rule <div>s (site first-app-norules). */
export function withoutRules(doc) {
  return doc.replace(/\s*<div hidden itemscope itemtype="https:\/\/pagelove.org\/AuthorizationRule">[\s\S]*?<\/div>/g, "");
}
