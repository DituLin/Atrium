import { useEffect, useRef, useState } from 'react';

/** No margin: off-screen library metadata must not prepare the whole page. */
export function useInViewport() {
  const ref = useRef<HTMLDivElement>(null);
  const [viewport, setViewport] = useState({ visible: false, version: 0 });
  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    const setVisible = (visible: boolean) => setViewport(current => current.visible === visible
      ? current : { visible, version: current.version + 1 });
    if (typeof IntersectionObserver === 'function') {
      const observer = new IntersectionObserver(entries => {
        for (const entry of entries) if (entry.target === element) setVisible(entry.isIntersecting);
      });
      observer.observe(element);
      return () => observer.disconnect();
    }
    // Older embedded engines can still restrict work to actual viewport bounds.
    const check = () => {
      const rect = element.getBoundingClientRect();
      setVisible(rect.width > 0 && rect.height > 0 && rect.bottom > 0 && rect.right > 0
        && rect.top < window.innerHeight && rect.left < window.innerWidth);
    };
    check();
    window.addEventListener('scroll', check, true);
    window.addEventListener('resize', check);
    return () => { window.removeEventListener('scroll', check, true); window.removeEventListener('resize', check); };
  }, []);
  return { ref, ...viewport };
}
