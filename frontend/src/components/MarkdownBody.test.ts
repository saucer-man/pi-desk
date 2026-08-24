import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import MarkdownBody from "./MarkdownBody.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({
  repositoryService: {
    previewFile: vi.fn(), openFile: vi.fn(), openFileWith: vi.fn(), saveFileAs: vi.fn(), revealFile: vi.fn(),
  },
}));

function mountMarkdown(text: string) {
  const pinia = createPinia();
  setActivePinia(pinia);
  const store = useAppStore();
  store.$patch({
    threads: [{
      id: "thread-1", title: "Files", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
      status: "idle", started: false, generation: 0,
    }],
    activeThreadId: "thread-1",
  });
  return { store, wrapper: mount(MarkdownBody, { props: { text }, attachTo: document.body, global: { plugins: [pinia] } }) };
}

describe("MarkdownBody", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    vi.clearAllMocks();
  });

  it("renders common Markdown while disabling raw HTML and unsafe links", () => {
    const { wrapper } = mountMarkdown("**Done**\n\n- one\n- two\n\n[bad](javascript:alert(1))\n\n<script>alert(1)</script>");

    expect(wrapper.find("strong").text()).toBe("Done");
    expect(wrapper.findAll("li")).toHaveLength(2);
    expect(wrapper.html()).not.toContain("href=\"javascript:");
    expect(wrapper.find("script").exists()).toBe(false);
    expect(wrapper.text()).toContain("<script>alert(1)</script>");
    wrapper.unmount();
  });

  it("turns workspace file links into preview and context-menu targets while leaving web links external", async () => {
    const { store, wrapper } = mountMarkdown("[result](reports/tg_groups.csv) and [web](https://example.com/docs)");
    store.openRepositoryFilePreview = vi.fn().mockResolvedValue(undefined);
    const links = wrapper.findAll("a");

    expect(links[0].classes()).toContain("markdown-file-link");
    expect(links[0].attributes("title")).toBe("D:\\repo\\reports\\tg_groups.csv");
    expect(links[0].attributes("href")).toBe("#");
    expect(links[1].attributes("target")).toBe("_blank");

    await links[0].trigger("click");
    expect(store.openRepositoryFilePreview).toHaveBeenCalledWith("reports/tg_groups.csv", undefined);

    await links[0].trigger("contextmenu", { clientX: 80, clientY: 90 });
    await flushPromises();
    const menu = document.body.querySelector<HTMLElement>('[role="menu"][aria-label="File actions"]');
    expect(menu?.textContent).toContain("Open file");
    expect(menu?.textContent).toContain("Open with...");
    expect(menu?.textContent).toContain("Save as...");
    expect(menu?.textContent).toContain("Copy path");
    expect(menu?.textContent).toContain("Show in file manager");
    wrapper.unmount();
  });

  it("highlights search matches in rendered Markdown", async () => {
    const { wrapper } = mountMarkdown("**Done** and done");
    await wrapper.setProps({ searchQuery: "done", searchActive: true });

    expect(wrapper.findAll("mark.markdown-search-hit")).toHaveLength(2);
    expect(wrapper.findAll("mark.is-active")).toHaveLength(2);
    wrapper.unmount();
  });
});
