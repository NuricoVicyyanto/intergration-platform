/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{html,js,svelte}'],
  theme: {
    extend: {
      colors: {
        ink: '#172033',
        navy: '#0f172a',
        muted: '#64748b',
        brand: '#2563eb',
        'brand-deep': '#1d4ed8',
        accent: '#0f766e',
        canvas: '#f5f7fa',
        line: '#e2e8f0'
      },
      boxShadow: {
        panel: '0 1px 2px rgba(15,23,42,.035)',
        modal: '0 24px 64px rgba(15,23,42,.2)'
      }
    }
  },
  plugins: []
};
