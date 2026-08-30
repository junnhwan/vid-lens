import type { Config } from 'tailwindcss'
import productConfig from '../tailwind.config'

const config: Config = {
  ...productConfig,
  content: [
    './app/**/*.{ts,tsx}',
    '../components/**/*.{ts,tsx}',
  ],
}

export default config
