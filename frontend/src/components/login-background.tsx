"use client";

import dynamic from "next/dynamic";
import { useSyncExternalStore } from "react";
import "./login-background.css";

const Topography = dynamic(() => import("./topography"), { ssr: false });
const reducedMotionQuery = "(prefers-reduced-motion: reduce)";

function subscribeToReducedMotion(onChange: () => void) {
  const preference = window.matchMedia(reducedMotionQuery);
  preference.addEventListener("change", onChange);
  return () => preference.removeEventListener("change", onChange);
}

function prefersReducedMotion() {
  return window.matchMedia(reducedMotionQuery).matches;
}

export default function LoginBackground() {
  const reducedMotion = useSyncExternalStore(subscribeToReducedMotion, prefersReducedMotion, () => true);

  return <div className="login-topography">
    {!reducedMotion && <Topography color="#25a7ed" speed={0.14} brightness={0.9} opacity={0.8} dpr={1} />}
  </div>;
}
