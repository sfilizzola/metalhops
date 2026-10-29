import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Check } from "lucide-react";
import { expect, test, vi } from "vitest";
import { Alert } from "./Alert";
import { Button } from "./Button";
import { Field } from "./Field";
import { GenderIcon } from "./GenderIcon";
import { Icon } from "./Icon";
import { Skeleton, Spinner } from "./Spinner";

test("Button applies variant and size classes and defaults to type=button", () => {
  render(<Button variant="danger" size="lg">Delete</Button>);
  const b = screen.getByRole("button", { name: "Delete" });
  expect(b).toHaveClass("btn", "btn--danger", "btn--lg");
  expect(b).toHaveAttribute("type", "button");
});

test("Button loading disables it, shows a spinner and blocks clicks", async () => {
  const onClick = vi.fn();
  render(<Button loading onClick={onClick}>Save</Button>);
  const b = screen.getByRole("button", { name: "Save" });
  expect(b).toBeDisabled();
  expect(b).toHaveAttribute("aria-busy", "true");
  expect(b.querySelector("svg.spinner")).toBeInTheDocument();
  await userEvent.click(b);
  expect(onClick).not.toHaveBeenCalled();
});

test("Field links label, help and error and flags invalid", () => {
  render(<Field id="u" label="Username" help="3-30 chars" error="Too short" />);
  const input = screen.getByLabelText("Username");
  expect(input).toHaveAttribute("aria-invalid", "true");
  expect(input).toHaveAttribute("aria-describedby", "u-error u-help");
  expect(screen.getByRole("alert")).toHaveTextContent("Too short");
  expect(screen.getByRole("alert")).toHaveAttribute("id", "u-error");
});

test("Field without error has no aria-invalid and passes input props", () => {
  render(<Field id="e" label="Email" type="email" disabled />);
  const input = screen.getByLabelText("Email");
  expect(input).not.toHaveAttribute("aria-invalid");
  expect(input).not.toHaveAttribute("aria-describedby");
  expect(input).toHaveAttribute("type", "email");
  expect(input).toBeDisabled();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});

test("Alert uses role alert for errors and status for success", () => {
  const { rerender } = render(<Alert kind="error">Bad</Alert>);
  expect(screen.getByRole("alert")).toHaveClass("form-alert");
  rerender(<Alert kind="success">Good</Alert>);
  expect(screen.getByRole("status")).toHaveTextContent("Good");
});

test("Icon is decorative and sized", () => {
  const { container } = render(<Icon icon={Check} size="lg" />);
  const svg = container.querySelector("svg")!;
  expect(svg).toHaveAttribute("aria-hidden", "true");
  expect(svg).toHaveClass("icon", "icon--lg");
  expect(svg).toHaveAttribute("width", "24");
});

test("Spinner and Skeleton render hidden decorative elements", () => {
  const { container } = render(<><Spinner /><Skeleton width="10px" /></>);
  expect(container.querySelector("svg.spinner")).toHaveAttribute("aria-hidden", "true");
  expect(container.querySelector(".skeleton")).toHaveStyle({ width: "10px" });
});

test("GenderIcon exposes an accessible name and tooltip and draws one decorative svg", () => {
  const { container, rerender } = render(<GenderIcon gender="non_binary" />);
  const img = screen.getByRole("img", { name: "Gender: Non-binary" });
  expect(img).toHaveAttribute("title", "Non-binary");
  expect(container.querySelectorAll("svg")).toHaveLength(1);
  expect(container.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
  for (const [g, name] of [["woman", "Woman"], ["man", "Man"], ["prefer_not_to_say", "Prefer not to say"]] as const) {
    rerender(<GenderIcon gender={g} />);
    expect(screen.getByRole("img", { name: `Gender: ${name}` })).toBeTruthy();
  }
});
