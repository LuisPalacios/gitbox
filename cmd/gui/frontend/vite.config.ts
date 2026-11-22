import {defineConfig} from 'vite'
import {svelte} from '@sveltejs/vite-plugin-svelte'
import icons from './vite-plugin-icons.ts'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [svelte(), icons()],
  build: {
    rolldownOptions: {
      output: {
        // The inlined logos (virtual:icons) and the English and Spanish text
        // catalogs (lib/i18n.ts) get chunks of their own, so the app chunk
        // stays under the bundler's 500 kB warning as logos and texts are
        // added. Everything ships inside the binary, so the split costs
        // nothing at load time.
        codeSplitting: {
          groups: [
            {name: 'icons', test: /virtual:icons/},
            {name: 'i18n', test: /src[\\/]lib[\\/]i18n\.ts$/},
          ],
        },
      },
    },
  },
})
