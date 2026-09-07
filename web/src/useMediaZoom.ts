import { useRef, useState, type TouchEvent } from "react";
export default function useMediaZoom() {
  const [view, setView] = useState({ scale: 1, x: 0, y: 0 });
  const current = useRef(view);
  current.current = view;
  const gesture = useRef<{
      distance: number;
      scale: number;
      x: number;
      y: number;
      mx: number;
      my: number;
      anchorX: number;
      anchorY: number;
    } | null>(null),
    consuming = useRef(false);
  const point = (e: TouchEvent) => {
    const t = e.touches;
    return {
      mx: t.length > 1 ? (t[0].clientX + t[1].clientX) / 2 : t[0].clientX,
      my: t.length > 1 ? (t[0].clientY + t[1].clientY) / 2 : t[0].clientY,
      distance:
        t.length > 1
          ? Math.hypot(t[0].clientX - t[1].clientX, t[0].clientY - t[1].clientY)
          : 0,
    };
  };
  const start = (e: TouchEvent) => {
    if ((e.target as HTMLElement).closest("button,input")) return false;
    if (e.touches.length === 2 || current.current.scale > 1) {
      const p = point(e),
        v = current.current,
        r = e.currentTarget.getBoundingClientRect();
      gesture.current = {
        ...p,
        ...v,
        anchorX: (p.mx - r.left - r.width / 2 - v.x) / v.scale,
        anchorY: (p.my - r.top - r.height / 2 - v.y) / v.scale,
      };
      consuming.current = true;
      return true;
    }
    return consuming.current;
  };
  const move = (e: TouchEvent) => {
    const g = gesture.current;
    if (!g) return consuming.current;
    const p = point(e),
      r = e.currentTarget.getBoundingClientRect();
    const scale =
      g.distance && e.touches.length === 2
        ? Math.min(4, Math.max(1, (g.scale * p.distance) / g.distance))
        : g.scale;
    const x = g.distance
      ? p.mx - r.left - r.width / 2 - g.anchorX * scale
      : g.x + p.mx - g.mx;
    const y = g.distance
      ? p.my - r.top - r.height / 2 - g.anchorY * scale
      : g.y + p.my - g.my;
    setView({
      scale,
      x: Math.max(
        (-r.width * (scale - 1)) / 2,
        Math.min((r.width * (scale - 1)) / 2, x),
      ),
      y: Math.max(
        (-r.height * (scale - 1)) / 2,
        Math.min((r.height * (scale - 1)) / 2, y),
      ),
    });
    return true;
  };
  const end = (e: TouchEvent) => {
    const used = consuming.current;
    if (e.touches.length === 0) {
      gesture.current = null;
      consuming.current = false;
    } else if (used) {
      gesture.current = null;
    }
    return used;
  };
  const reset = () => {
    setView({ scale: 1, x: 0, y: 0 });
    gesture.current = null;
    consuming.current = false;
  };
  return {
    view,
    start,
    move,
    end,
    reset,
    style: {
      transform: `translate(${view.x}px,${view.y}px) scale(${view.scale})`,
    },
  };
}
