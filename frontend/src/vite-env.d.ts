/// <reference types="vite/client" />

declare module "*.jpg" {
  const src: string
  export default src
}

declare module "*.svg?raw" {
  const src: string
  export default src
}
