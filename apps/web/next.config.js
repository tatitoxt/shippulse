/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  async rewrites() {
    return [
      {
        source: '/api/v1/:path*',
        destination: `${process.env.CONTROL_PLANE_URL || 'http://control-plane:8080'}/api/v1/:path*`,
      },
    ]
  },
}

module.exports = nextConfig
