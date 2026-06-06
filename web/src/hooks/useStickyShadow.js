import { useCallback, useEffect, useRef } from 'react';

// 监听 scroll 容器的横向溢出与滚动位置，在容器元素上写入 CSS 变量
// --sticky-shadow-opacity：0 = 已滚到最右或不溢出，0.1 = 仍有右侧内容被遮挡。
// 配合 ui-component/stickyCellSx 的 var(--sticky-shadow-opacity, 0.1) 兜底值，
// 即未注入容器 ref 的页面阴影行为与旧版一致。
export default function useStickyShadow() {
  const elRef = useRef(null);
  const cleanupRef = useRef(null);

  const update = useCallback(() => {
    const el = elRef.current;
    if (!el) return;
    const overflowed = el.scrollWidth > el.clientWidth + 1;
    const atRight = el.scrollLeft + el.clientWidth >= el.scrollWidth - 1;
    el.style.setProperty('--sticky-shadow-opacity', overflowed && !atRight ? '0.1' : '0');
  }, []);

  const containerRef = useCallback(
    (node) => {
      if (cleanupRef.current) {
        cleanupRef.current();
        cleanupRef.current = null;
      }
      elRef.current = node;
      if (!node) return;
      update();
      node.addEventListener('scroll', update, { passive: true });
      const ro = new ResizeObserver(update);
      ro.observe(node);
      cleanupRef.current = () => {
        node.removeEventListener('scroll', update);
        ro.disconnect();
      };
    },
    [update]
  );

  useEffect(() => {
    return () => {
      if (cleanupRef.current) {
        cleanupRef.current();
        cleanupRef.current = null;
      }
    };
  }, []);

  return containerRef;
}
