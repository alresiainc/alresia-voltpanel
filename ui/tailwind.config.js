export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // Volt's signature accent -- used sparingly (logo mark, active nav
        // rail, primary buttons, focus rings) against otherwise neutral
        // slate. Keeps the "electric" brand cue without tipping into a
        // colorful SaaS-template look.
        volt: {
          50: '#fffbeb',
          100: '#fef3c7',
          200: '#fde68a',
          300: '#fcd34d',
          400: '#fbbf24',
          500: '#f59e0b',
          600: '#d97706',
          700: '#b45309',
        },
      },
      boxShadow: {
        card: '0 1px 2px 0 rgb(15 23 42 / 0.04), 0 1px 1px 0 rgb(15 23 42 / 0.03)',
      },
      fontFamily: {
        // System font stack only -- VoltPanel is local-first and shouldn't
        // depend on a network fetch for a font just to render its own UI.
        sans: ['ui-sans-serif', 'system-ui', '-apple-system', 'Segoe UI', 'Roboto', 'sans-serif'],
      },
    },
  },
  plugins: [],
}
