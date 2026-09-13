import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import PiDeskGoalPanel from "./PiDeskGoalPanel.vue";
import type { GoalWidgetProjection } from "../utils/goalWidget";

describe("PiDeskGoalPanel", () => {
  it("renders an active goal with status, budget progress, and a pause action", async () => {
    const goal: GoalWidgetProjection = {
      status: "active",
      iteration: 3,
      tokensUsed: 12000,
      tokenBudget: 50000,
      text: "Ship the release\nand verify CI",
    };
    const wrapper = mount(PiDeskGoalPanel, { props: { goal } });

    expect(wrapper.classes()).toContain("is-active");
    expect(wrapper.get(".pi-desk-goal-heading").text()).toContain("Active");
    expect(wrapper.get(".pi-desk-goal-heading").text()).toContain("Iteration 3");
    expect(wrapper.get(".pi-desk-goal-text").text()).toContain("Ship the release");
    expect(wrapper.get("footer").text()).toContain("12000 of 50000 tokens used");
    expect(Number.parseFloat(wrapper.get<HTMLElement>(".pi-desk-goal-budget span").element.style.width)).toBeCloseTo(24, 0);

    await wrapper.get('button[title="Pause goal"]').trigger("click");
    expect(wrapper.emitted("command")).toEqual([["/goal pause"]]);
  });

  it("offers resume for a paused goal and hides clear when complete", async () => {
    const wrapper = mount(PiDeskGoalPanel, {
      props: { goal: { status: "paused", iteration: 2, tokensUsed: 900, text: "Half done" } satisfies GoalWidgetProjection },
    });

    expect(wrapper.classes()).toContain("is-paused");
    await wrapper.get('button[title="Resume goal"]').trigger("click");
    expect(wrapper.emitted("command")).toEqual([["/goal resume"]]);
    expect(wrapper.find('button[title="Clear goal"]').exists()).toBe(true);

    const complete = mount(PiDeskGoalPanel, {
      props: { goal: { status: "complete", iteration: 4, tokensUsed: 2000, text: "Done" } satisfies GoalWidgetProjection },
    });
    expect(complete.classes()).toContain("is-complete");
    expect(complete.find('button[title="Clear goal"]').exists()).toBe(false);
  });

  it("clears only after an armed confirmation click", async () => {
    const wrapper = mount(PiDeskGoalPanel, {
      props: { goal: { status: "active", iteration: 1, tokensUsed: 10, text: "Work" } satisfies GoalWidgetProjection },
    });

    const clear = wrapper.get('button[title="Clear goal"]');
    await clear.trigger("click");
    expect(wrapper.emitted("command")).toBeUndefined();
    expect(clear.attributes("title")).toBe("Confirm clear goal");

    await clear.trigger("click");
    expect(wrapper.emitted("command")).toEqual([["/goal clear"]]);
  });
});
