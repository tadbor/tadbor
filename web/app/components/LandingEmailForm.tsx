"use client";

import { useState } from "react";

export default function LandingEmailForm() {
  const [msg, setMsg] = useState("");
  const [isError, setIsError] = useState(false);

  function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const input = e.currentTarget.elements.namedItem("email") as HTMLInputElement;
    const email = input.value.trim();
    if (!email || !/^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/.test(email)) {
      setMsg("Enter a valid email first.");
      setIsError(true);
      return;
    }
    setMsg("You're on the list — thanks!");
    setIsError(false);
    input.value = "";
  }

  return (
    <form onSubmit={onSubmit} className="early-form">
      <input
        id="emailInput"
        name="email"
        type="email"
        required
        placeholder="name@email.com"
        className="early-input"
      />
      <button type="submit" className="btn btn-primary btn-hero">
        Notify me
      </button>
      {msg && (
        <p className="form-msg" style={{ color: isError ? "#B54A3C" : "var(--primary)" }}>
          {msg}
        </p>
      )}
      {!msg && <p className="form-msg"></p>}
    </form>
  );
}