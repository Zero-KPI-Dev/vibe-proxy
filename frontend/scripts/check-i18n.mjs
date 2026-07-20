import assert from "node:assert/strict"
import { readFile, readdir } from "node:fs/promises"

const locales = {
  en: JSON.parse(await readFile(new URL("../src/locales/en.json", import.meta.url), "utf8")),
  "zh-CN": JSON.parse(await readFile(new URL("../src/locales/zh-CN.json", import.meta.url), "utf8")),
}

function flatten(value, prefix = "", result = {}) {
  for (const [key, child] of Object.entries(value)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (child && typeof child === "object" && !Array.isArray(child)) {
      flatten(child, path, result)
    } else {
      result[path] = child
    }
  }
  return result
}

const english = flatten(locales.en)
const chinese = flatten(locales["zh-CN"])

assert.deepEqual(Object.keys(chinese).sort(), Object.keys(english).sort(), "Locale keys must match")

for (const [locale, resources] of Object.entries(locales)) {
  for (const [key, value] of Object.entries(flatten(resources))) {
    assert.equal(typeof value, "string", `${locale}:${key} must be a string`)
    assert.notEqual(value.trim(), "", `${locale}:${key} must not be empty`)
  }
}

async function sourceFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true })
  const files = []
  for (const entry of entries) {
    const path = new URL(`${entry.name}${entry.isDirectory() ? "/" : ""}`, directory)
    if (entry.isDirectory()) files.push(...await sourceFiles(path))
    else if (/\.(ts|tsx)$/.test(entry.name)) files.push(path)
  }
  return files
}

const sourceRoot = new URL("../src/", import.meta.url)
const missingKeys = []
for (const file of await sourceFiles(sourceRoot)) {
  const source = await readFile(file, "utf8")
  for (const match of source.matchAll(/\bt\(["']([^"']+)["']/g)) {
    const key = match[1]
    if (!key.includes("${") && !(key in english)) {
      missingKeys.push(`${file.pathname}:${key}`)
    }
  }
}
assert.deepEqual(missingKeys, [], "All statically referenced translation keys must exist")

console.log(`i18n resources OK: ${Object.keys(english).length} keys across ${Object.keys(locales).length} locales`)
