import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

test.use({ baseURL: process.env.E2E_BASE_URL })

test('publish and delete a site', async ({ page, request }) => {
  const dir = mkdtempSync(join(tmpdir(), 'static-host-site-'))
  let keyID = ''
  let siteID = ''
  try {
    writeFileSync(join(dir, 'index.html'), '<h1>Published fixture</h1>')
    execFileSync('tar', ['-czf', join(dir, 'site.tar.gz'), '-C', dir, 'index.html'])
    await page.goto('/login')
    await page.getByLabel('Логин').fill('admin')
    await page.getByLabel('Пароль').fill('e2e-test-password')
    await page.getByRole('button', { name: 'Войти' }).click()
    await expect(page.getByRole('heading', { name: 'Сайты' })).toBeVisible()
    await expect(page.getByText('Сайтов пока нет')).toBeVisible()
    await page.route('**/api/sites', route => route.abort())
    await page.getByRole('button', { name: 'Обновить список' }).click()
    await expect(page.getByRole('heading', { name: 'Ключи агентов' })).toBeVisible()
    await expect(page.getByRole('alert')).toBeVisible()
    await page.unroute('**/api/sites')
    const session = await (await page.request.get('/api/session')).json()
    const origin = new URL(page.url()).origin
    const headers = { Origin: origin, 'X-CSRF-Token': session.csrf_token }
    const keyResponse = await page.request.post('/api/keys', { headers, data: { name: 'Sites E2E', expires_in_days: null } })
    expect(keyResponse.status()).toBe(201)
    const key = await keyResponse.json(); keyID = key.key.id
    const mcpHeaders = { Authorization: `Bearer ${key.token}`, Accept: 'application/json, text/event-stream', 'MCP-Protocol-Version': '2025-11-25' }
    async function call(name:string, args:Record<string,unknown>) {
      const response = await request.post('/mcp', { headers: mcpHeaders, data: { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } } })
      expect(response.status()).toBe(200)
      const body = await response.json(); expect(body.result.isError).not.toBe(true); return body.result.structuredContent
    }
    const created = await call('sites_create', { name: 'Browser fixture' }); siteID = created.site.id
    await page.getByRole('button', { name: 'Обновить список' }).click()
    await expect(page.getByText('Browser fixture', { exact: true })).toBeVisible()
    await expect(page.getByText('Не опубликован', { exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: created.site.url })).toHaveCount(0)
    const grant = await call('uploads_create', { site_id: siteID, format: 'tar.gz', expected_generation: 0 })
    const upload = await request.put(grant.upload_url, { headers: { Authorization: `Bearer ${key.token}`, 'Content-Type': 'application/gzip' }, data: readFileSync(join(dir, 'site.tar.gz')) })
    expect(upload.status()).toBe(200)
    await call('sites_publish', { site_id: siteID, upload_id: grant.upload_id, expected_generation: 0, idempotency_key: 'e2e-publish' })
    await page.getByRole('button', { name: 'Обновить список' }).click()
    await expect(page.getByText('Browser fixture', { exact: true })).toBeVisible()
    const popupPromise = page.waitForEvent('popup')
    await page.getByRole('link', { name: created.site.url }).click()
    const popup = await popupPromise; await expect(popup.getByText('Published fixture')).toBeVisible(); await popup.close()
    expect((await page.request.delete(`/api/sites/${siteID}`, { headers: { Origin: origin } })).status()).toBe(403)
    expect((await page.request.delete(`/api/sites/${siteID}`, { headers: { Origin: 'https://attacker.invalid', 'X-CSRF-Token': session.csrf_token } })).status()).toBe(403)
    await page.getByRole('button', { name: 'Удалить сайт Browser fixture' }).click()
    await page.getByRole('dialog', { name: 'Удалить сайт?' }).getByRole('button', { name: 'Удалить' }).click()
    await expect(page.getByText('Browser fixture', { exact: true })).toHaveCount(0)
    expect((await request.get(created.site.url)).status()).toBe(404); siteID = ''
  } finally {
    if (siteID) { const session = await (await page.request.get('/api/session')).json(); await page.request.delete(`/api/sites/${siteID}`, { headers: { Origin: new URL(page.url()).origin, 'X-CSRF-Token': session.csrf_token } }) }
    if (keyID) { const session = await (await page.request.get('/api/session')).json(); await page.request.delete(`/api/keys/${keyID}`, { headers: { Origin: new URL(page.url()).origin, 'X-CSRF-Token': session.csrf_token } }) }
    rmSync(dir, { recursive: true, force: true })
  }
})
