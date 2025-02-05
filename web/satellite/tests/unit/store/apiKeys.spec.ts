// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

import Vuex from 'vuex';

import { ApiKeysApiGql } from '@/api/apiKeys';
import { ProjectsApiGql } from '@/api/projects';
import { API_KEYS_MUTATIONS, makeApiKeysModule } from '@/store/modules/apiKeys';
import { makeProjectsModule } from '@/store/modules/projects';
import { ApiKey, ApiKeyOrderBy, ApiKeysPage } from '@/types/apiKeys';
import { SortDirection } from '@/types/common';
import { Project } from '@/types/projects';
import { API_KEYS_ACTIONS } from '@/utils/constants/actionNames';
import { createLocalVue } from '@vue/test-utils';

const Vue = createLocalVue();
const apiKeysApi = new ApiKeysApiGql();
const apiKeysModule = makeApiKeysModule(apiKeysApi);
const {FETCH, CREATE, CLEAR_SELECTION, DELETE, TOGGLE_SELECTION, CLEAR} = API_KEYS_ACTIONS;

const projectsApi = new ProjectsApiGql();
const projectsModule = makeProjectsModule(projectsApi);
const selectedProject = new Project('', '', '', '');
selectedProject.id = '1';
projectsModule.state.selectedProject = selectedProject;

const apiKey = new ApiKey('testId', 'testName', 'testCreatedAt', 'testSecret');
const apiKey2 = new ApiKey('testId2', 'testName2', 'testCreatedAt2', 'testSecret2');

const FIRST_PAGE = 1;
const TEST_ERROR = 'testError';
const UNREACHABLE_ERROR = 'should be unreachable';

Vue.use(Vuex);

const store = new Vuex.Store({modules: {projectsModule, apiKeysModule}});

const state = (store.state as any).apiKeysModule;

describe('mutations', () => {
    it('fetch api keys', function () {
        const testApiKeysPage = new ApiKeysPage();
        testApiKeysPage.apiKeys = [apiKey];
        testApiKeysPage.totalCount = 1;
        testApiKeysPage.pageCount = 1;

        store.commit(API_KEYS_MUTATIONS.SET_PAGE, testApiKeysPage);

        expect(state.page.apiKeys.length).toBe(1);
        expect(state.page.search).toBe('');
        expect(state.page.order).toBe(ApiKeyOrderBy.NAME);
        expect(state.page.orderDirection).toBe(SortDirection.ASCENDING);
        expect(state.page.limit).toBe(6);
        expect(state.page.pageCount).toBe(1);
        expect(state.page.currentPage).toBe(1);
        expect(state.page.totalCount).toBe(1);
    });

    it('set api keys page', function () {
        store.commit(API_KEYS_MUTATIONS.SET_PAGE_NUMBER, 2);

        expect(state.cursor.page).toBe(2);
    });

    it('set search query', function () {
        store.commit(API_KEYS_MUTATIONS.SET_SEARCH_QUERY, 'testSearchQuery');

        expect(state.cursor.search).toBe('testSearchQuery');
    });

    it('set sort order', function () {
        store.commit(API_KEYS_MUTATIONS.CHANGE_SORT_ORDER, ApiKeyOrderBy.EMAIL);

        expect(state.cursor.order).toBe(ApiKeyOrderBy.EMAIL);
    });

    it('set sort direction', function () {
        store.commit(API_KEYS_MUTATIONS.CHANGE_SORT_ORDER_DIRECTION, SortDirection.DESCENDING);

        expect(state.cursor.orderDirection).toBe(SortDirection.DESCENDING);
    });

    it('toggle selection', function () {
        store.commit(API_KEYS_MUTATIONS.TOGGLE_SELECTION, apiKey.id);

        expect(state.page.apiKeys[0].isSelected).toBe(true);
    });

    it('clear selection', function () {
        store.commit(API_KEYS_MUTATIONS.CLEAR_SELECTION);

        state.page.apiKeys.forEach((key: ApiKey) => {
            expect(key.isSelected).toBe(false);
        });
    });

    it('clear store', function () {
        store.commit(API_KEYS_MUTATIONS.CLEAR);

        expect(state.cursor.page).toBe(1);
        expect(state.cursor.search).toBe('');
        expect(state.cursor.order).toBe(ApiKeyOrderBy.NAME);
        expect(state.cursor.orderDirection).toBe(SortDirection.ASCENDING);
        expect(state.page.apiKeys.length).toBe(0);
    });
});

describe('actions', () => {
    beforeEach(() => {
        jest.resetAllMocks();
    });

    it('success fetch apiKeys', async () => {
        const testApiKeysPage = new ApiKeysPage();
        testApiKeysPage.apiKeys = [apiKey];
        testApiKeysPage.totalCount = 1;
        testApiKeysPage.pageCount = 1;

        jest.spyOn(apiKeysApi, 'get').mockReturnValue(
            Promise.resolve(testApiKeysPage),
        );

        await store.dispatch(FETCH, FIRST_PAGE);

        expect(state.page.apiKeys[0].id).toBe(apiKey.id);
        expect(state.page.apiKeys[0].name).toBe(apiKey.name);
        expect(state.page.apiKeys[0].createdAt).toBe(apiKey.createdAt);
        expect(state.page.apiKeys[0].secret).toBe(apiKey.secret);
    });

    it('fetch throws an error when api call fails', async () => {
        jest.spyOn(apiKeysApi, 'get').mockImplementation(() => {
            throw new Error(TEST_ERROR);
        });

        try {
            await store.dispatch(FETCH);
        } catch (error) {
            store.commit(API_KEYS_MUTATIONS.CHANGE_SORT_ORDER_DIRECTION, SortDirection.DESCENDING);
            expect(error.message).toBe(TEST_ERROR);

            return;
        }

        fail(UNREACHABLE_ERROR);
    });

    it('success create apiKeys', async () => {
        jest.spyOn(apiKeysApi, 'create').mockReturnValue(Promise.resolve(apiKey));

        try {
            await store.dispatch(CREATE, 'testName');
            throw new Error(TEST_ERROR);
        } catch (error) {
            expect(error.message).toBe(TEST_ERROR);
        }
    });

    it('create throws an error when api call fails', async () => {
        jest.spyOn(apiKeysApi, 'create').mockImplementation(() => {
            throw new Error(TEST_ERROR);
        });

        try {
            await store.dispatch(CREATE, 'testName');
        } catch (error) {
            expect(error.message).toBe(TEST_ERROR);

            return;
        }

        fail(UNREACHABLE_ERROR);
    });

    it('success delete apiKeys', async () => {
        jest.spyOn(apiKeysApi, 'delete').mockReturnValue(
            Promise.resolve(),
        );

        try {
            await store.dispatch(DELETE, ['testId', 'testId']);
            throw new Error(TEST_ERROR);
        } catch (error) {
            expect(error.message).toBe(TEST_ERROR);
        }
    });

    it('delete throws an error when api call fails', async () => {
        jest.spyOn(apiKeysApi, 'delete').mockImplementation(() => {
            throw new Error(TEST_ERROR);
        });

        try {
            await store.dispatch(DELETE, 'testId');
        } catch (error) {
            expect(error.message).toBe(TEST_ERROR);

            return;
        }

        fail(UNREACHABLE_ERROR);
    });

    it('set api keys search query', async function () {
        await store.dispatch(API_KEYS_ACTIONS.SET_SEARCH_QUERY, 'search');

        expect(state.cursor.search).toBe('search');
    });

    it('set api keys sort by', async function () {
        await store.dispatch(API_KEYS_ACTIONS.SET_SORT_BY, ApiKeyOrderBy.CREATED_AT);

        expect(state.cursor.order).toBe(ApiKeyOrderBy.CREATED_AT);
    });

    it('set sort direction', async function () {
        await store.dispatch(API_KEYS_ACTIONS.SET_SORT_DIRECTION, SortDirection.DESCENDING);

        expect(state.cursor.orderDirection).toBe(SortDirection.DESCENDING);
    });

    it('success toggleAPIKeySelection apiKeys', async () => {
        jest.spyOn(apiKeysApi, 'get').mockReturnValue(
            Promise.resolve(new ApiKeysPage([apiKey, apiKey2],
                '',
                ApiKeyOrderBy.NAME,
                SortDirection.ASCENDING,
                6,
                2,
                1,
                2,
            )),
        );

        await store.dispatch(API_KEYS_ACTIONS.FETCH, FIRST_PAGE);

        await store.dispatch(API_KEYS_ACTIONS.TOGGLE_SELECTION, apiKey.id);

        expect(state.page.apiKeys[0].isSelected).toBe(true);
    });

    it('success clearSelection apiKeys', async () => {
        await store.dispatch(CLEAR_SELECTION);

        state.page.apiKeys.forEach((key: ApiKey) => {
            expect(key.isSelected).toBe(false);
        });
    });

    it('success clearAPIKeys', async () => {
        await store.dispatch(CLEAR);

        expect(state.cursor.search).toBe('');
        expect(state.cursor.limit).toBe(6);
        expect(state.cursor.page).toBe(1);
        expect(state.cursor.order).toBe(ApiKeyOrderBy.NAME);
        expect(state.cursor.orderDirection).toBe(SortDirection.ASCENDING);

        expect(state.page.apiKeys.length).toBe(0);
        expect(state.page.search).toBe('');
        expect(state.page.order).toBe(ApiKeyOrderBy.NAME);
        expect(state.page.orderDirection).toBe(SortDirection.ASCENDING);
        expect(state.page.limit).toBe(6);
        expect(state.page.pageCount).toBe(0);
        expect(state.page.currentPage).toBe(1);
        expect(state.page.totalCount).toBe(0);
    });
});

describe('getters', () => {
    const selectedApiKey = new ApiKey('testtestId', 'testtestName', 'testtestCreatedAt', 'testtestSecret');
    selectedApiKey.isSelected = true;

    it('selected apiKeys', () => {
        const testApiKeysPage = new ApiKeysPage();
        testApiKeysPage.apiKeys = [selectedApiKey];
        testApiKeysPage.totalCount = 1;
        testApiKeysPage.pageCount = 1;

        store.commit(API_KEYS_MUTATIONS.SET_PAGE, testApiKeysPage);

        const retrievedApiKeys = store.getters.selectedApiKeys;

        expect(retrievedApiKeys[0].id).toBe('testtestId');
    });

    it('apiKeys array', () => {
        const retrievedApiKeys = store.getters.selectedApiKeys;

        expect(retrievedApiKeys).toEqual([selectedApiKey]);
    });
});
