import {defineConfig} from 'vite'
import {svelte} from '@sveltejs/vite-plugin-svelte'
import icons from './vite-plugin-icons.ts'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [svelte(), icons()]
})
