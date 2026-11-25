import { startTestServer } from './server.mjs'

export default async function globalSetup() {
  const server = await startTestServer()
  process.env.E2E_BASE_URL = server.baseURL
  return async () => server.stop()
}
