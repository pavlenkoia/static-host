import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { spawn } from 'node:child_process'
import { createServer } from 'node:net'
import { setTimeout as delay } from 'node:timers/promises'

const root = resolve(import.meta.dirname, '../..')

async function freePort() {
  while (true) {
    const server = createServer()
    await new Promise((resolve, reject) => server.once('error', reject).listen(0, '127.0.0.1', resolve))
    const { port } = server.address()
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()))
    if (port !== 8081) return port
  }
}

function run(command, args, options) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { stdio: ['ignore', 'pipe', 'pipe'], ...options })
    let output = ''
    child.stdout.on('data', chunk => { output += chunk })
    child.stderr.on('data', chunk => { output += chunk })
    child.once('error', reject)
    child.once('exit', (code, signal) => code === 0 ? resolve(output) : reject(new Error(`${command} exited ${code ?? signal}: ${output}`)))
  })
}

export async function startTestServer() {
  const directory = await mkdtemp(join(tmpdir(), 'static-host-e2e-'))
  const port = await freePort()
  const binary = join(directory, process.platform === 'win32' ? 'static-host.exe' : 'static-host')
  const env = { ...process.env, STATIC_HOST_ADMIN_PASSWORD: 'e2e-test-password' }
  for (const key of ['STATIC_HOST_LISTEN', 'STATIC_HOST_DATA_DIR', 'STATIC_HOST_BASE_DOMAIN', 'STATIC_HOST_DOMAIN_INGRESS_IPS', 'STATIC_HOST_DOMAIN_RESERVED_HOSTS']) delete env[key]
  env.STATIC_HOST_DOMAIN_INGRESS_IPS='127.0.0.1'
  let child
  try {
    await run('go', ['build', '-o', binary, './cmd/static-host'], { cwd: root })
    child = spawn(binary, ['--data-dir', directory, '--listen', `127.0.0.1:${port}`], { cwd: root, env, stdio: ['ignore', 'ignore', 'pipe'] })
    let stderr = ''
    child.stderr.on('data', chunk => { stderr += chunk })
    const baseURL = `http://127.0.0.1:${port}`
    let ready = false
    for (let attempt = 0; attempt < 100; attempt++) {
      if (child.exitCode !== null) throw new Error(`test server exited ${child.exitCode}: ${stderr}`)
      try {
        const response = await fetch(`${baseURL}/healthz`)
        if (response.ok) { ready = true; break }
      } catch {}
      await delay(100)
    }
    if (!ready) throw new Error(`test server did not become ready: ${stderr}`)
    return {
      baseURL,
      async stop() {
        if (child.exitCode === null) {
          child.kill('SIGTERM')
          const exited = new Promise(resolve => child.once('exit', resolve))
          await Promise.race([exited, delay(5000)])
          if (child.exitCode === null) {
            child.kill('SIGKILL')
            await exited
          }
        }
        await rm(directory, { recursive: true, force: true })
      },
    }
  } catch (error) {
    if (child && child.exitCode === null) {
      child.kill('SIGKILL')
      await new Promise(resolve => child.once('exit', resolve))
    }
    await rm(directory, { recursive: true, force: true })
    throw error
  }
}
