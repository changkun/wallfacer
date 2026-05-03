import { onMounted } from 'vue';

export function useReveal() {
  onMounted(() => {
    if (import.meta.env.SSR) return;
    if (typeof IntersectionObserver === 'undefined') {
      document.querySelectorAll('.reveal').forEach(el => el.classList.add('revealed'));
      return;
    }

    const selectors = [
      '.product-card', '.hero-content', '.hero-title', '.hero-sub',
      '.hero-actions', '.philosophy-lead', '.philosophy-body',
      '.mission-block', '.mission-text', '.section-label',
      '.product-hero-text', '.product-hero-image',
      '.features-grid', '.timeline-step', '.tour-item',
      '.capability-grid', '.values-grid', '.team-member',
      '.pricing-grid', '.download-grid', '.docs-grid',
      '.blog-listing', '.podcast-listing', '.contact-block',
      '.def-list', '.legal-content',
    ];

    const observer = new IntersectionObserver((entries) => {
      entries.forEach(entry => {
        if (entry.isIntersecting) {
          const el = entry.target as HTMLElement;
          el.classList.add('revealed');
          if (el.classList.contains('features-grid') ||
              el.classList.contains('values-grid') ||
              el.classList.contains('capability-grid') ||
              el.classList.contains('pricing-grid') ||
              el.classList.contains('download-grid') ||
              el.classList.contains('docs-grid')) {
            Array.from(el.children).forEach((child, i) => {
              (child as HTMLElement).style.transitionDelay = `${i * 60}ms`;
            });
          }
          observer.unobserve(el);
        }
      });
    }, { threshold: 0.15, rootMargin: '0px 0px -40px 0px' });

    selectors.forEach(sel => {
      document.querySelectorAll(sel).forEach(el => {
        el.classList.add('reveal');
        observer.observe(el);
      });
    });
  });
}
