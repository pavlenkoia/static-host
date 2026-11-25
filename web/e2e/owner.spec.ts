import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

test.use({ baseURL: process.env.E2E_BASE_URL })

const owner = 'admin'
const password = 'e2e-test-password'

async function login(page: Page) {
  await page.getByLabel('Логин').fill(owner)
  await page.getByLabel('Пароль').fill(password)
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page.getByRole('heading', { name: 'Ключи агентов' })).toBeVisible()
}

test('owner login, API protections, and agent key lifecycle', async ({ page, request }) => {
  await page.goto('/login')
  await page.getByLabel('Логин').fill(owner)
  await page.getByLabel('Пароль').fill('incorrect-password')
  await page.getByRole('button', { name: 'Войти' }).click()
  await expect(page.getByText('Неверный логин или пароль')).toBeVisible()

  await login(page)
  const noCSRF = await page.request.post('/api/keys', {
    data: { name: 'forbidden', expires_in_days: null },
    headers: { Origin: new URL(page.url()).origin },
  })
  expect(noCSRF.status()).toBe(403)
  const session = await (await page.request.get('/api/session')).json()
  const foreignOrigin = await page.request.post('/api/keys', {
    data: { name: 'forbidden', expires_in_days: null },
    headers: { Origin: 'https://attacker.invalid', 'X-CSRF-Token': session.csrf_token },
  })
  expect(foreignOrigin.status()).toBe(403)

  await page.getByRole('button', { name: 'Создать ключ' }).first().click()
  await page.getByLabel('Название').fill('Browser agent')
  await page.getByRole('button', { name: 'Создать ключ' }).last().click()
  await expect(page.getByText('Browser agent', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Показать' }).click()
  const secret = page.getByRole('textbox', { name: 'Значение ключа Browser agent' })
  await expect(secret).toBeVisible()
  const token = await secret.inputValue()
  expect(token).toBeTruthy()
  await page.getByRole('button', { name: 'Скрыть' }).click()
  await expect(secret).toHaveCount(0)

  await page.reload()
  await expect(page.getByText('Browser agent', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Показать' }).click()
  await expect(secret).toHaveValue(token)

  const mcp = async () => request.post('/mcp', {
    data: { jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2025-11-25', capabilities: {}, clientInfo: { name: 'browser-e2e', version: '1.0' } } },
    headers: { Authorization: `Bearer ${token}`, Accept: 'application/json, text/event-stream' },
  })
  expect((await mcp()).status()).toBe(200)

  await page.getByRole('button', { name: 'Отозвать' }).click()
  await expect(page.getByRole('dialog', { name: 'Отозвать ключ?' })).toBeVisible()
  await page.getByRole('dialog').getByRole('button', { name: 'Отозвать' }).click()
  await expect(page.getByText('Отозван', { exact: true })).toBeVisible()
  expect((await mcp()).status()).toBe(401)

  await page.getByRole('button', { name: 'Удалить' }).click()
  await expect(page.getByRole('dialog', { name: 'Удалить ключ?' })).toBeVisible()
  await page.getByRole('dialog').getByRole('button', { name: 'Удалить' }).click()
  await expect(page.getByText('Browser agent', { exact: true })).toHaveCount(0)

  await page.getByRole('button', { name: 'Выйти' }).click()
  await expect(page.getByRole('heading', { name: 'Вход в кабинет' })).toBeVisible()
  expect((await page.request.get('/api/keys')).status()).toBe(401)
  expect((await page.request.get('/api/session')).status()).toBe(401)
})
