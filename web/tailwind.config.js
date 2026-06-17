/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // Mapped to CSS custom properties from styles/themes.css.
        // Using HSL space-separated channel format with `hsl()` wrapper.
        'surface-primary': 'hsl(var(--background) / <alpha-value>)',
        'surface-secondary': 'hsl(var(--card) / <alpha-value>)',
        'surface-tertiary': 'hsl(var(--secondary) / <alpha-value>)',
        'text-primary': 'hsl(var(--foreground) / <alpha-value>)',
        'text-secondary': 'hsl(var(--muted-foreground) / <alpha-value>)',
        'text-muted': 'hsl(var(--muted-foreground) / <alpha-value>)',
        'border-subtle': 'hsl(var(--border) / <alpha-value>)',
        'border-strong': 'hsl(var(--border) / 0.8)',
        accent: 'hsl(var(--primary) / <alpha-value>)',
        'accent-hover': 'hsl(var(--primary) / 0.9)',
        // Semantic status colors
        success: 'hsl(142 71% 45%)',
        warning: 'hsl(38 92% 50%)',
        danger: 'hsl(0 84% 60%)',
        info: 'hsl(217 91% 60%)',
      },
      borderRadius: {
        theme: 'var(--radius)',
      },
    },
  },
  plugins: [],
};
