import { defineConfig, loadEnv } from 'vite'
import type { Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import { publicClinicConfig } from './src/lib/bootConfig.ts'

function clinicConfigPlugin(): Plugin {
  let source: string
  return {
    name: 'clinic-public-config',
    configResolved(config) {
      source = JSON.stringify(publicClinicConfig(loadEnv(config.mode, config.envDir, 'VITE_')))
    },
    generateBundle() {
      this.emitFile({ type: 'asset', fileName: 'clinic-config.json', source })
    },
    configureServer(server) {
      server.middlewares.use('/clinic-config.json', (_req, res) => {
        res.setHeader('Content-Type', 'application/json')
        res.setHeader('Cache-Control', 'no-store')
        res.end(source)
      })
    },
  }
}

export default defineConfig({
  plugins: [react(), clinicConfigPlugin()],
  server: {
    port: 5173,
    strictPort: true,
  },
})
