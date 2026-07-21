import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'

const versionsDir = path.resolve('versions')
const entries = fs.readdirSync(versionsDir, { withFileTypes: true })

const versions = entries
  .filter(d => d.isDirectory() && d.name !== 'latest')
  .map(d => d.name)
  .sort((a, b) => a.localeCompare(b, undefined, { numeric: true }))

if (versions.length === 0) {
  console.error('No versions found in versions')
  process.exit(1)
}

const latest = versions.at(-1)
const latestPath = path.join(versionsDir, 'latest')

try {
  if (fs.existsSync(latestPath)) {
    fs.rmSync(latestPath, { recursive: true, force: true })
  }
  fs.symlinkSync(latest, latestPath, 'dir')
  console.log(`latest → ${latest}`)
} catch (err) {
  console.error('Failed to update latest symlink:', err)
  process.exit(1)
}
